package animation

import (
	"errors"
	"fmt"
	"net"
	"sync"
	"sync/atomic"
	"time"

	"github.com/kartFr/Asset-Reuploader/internal/app/assets/shared/assetutils"
	"github.com/kartFr/Asset-Reuploader/internal/app/assets/shared/clientutils"
	"github.com/kartFr/Asset-Reuploader/internal/app/assets/shared/uploaderror"
	"github.com/kartFr/Asset-Reuploader/internal/app/context"
	"github.com/kartFr/Asset-Reuploader/internal/app/request"
	"github.com/kartFr/Asset-Reuploader/internal/app/response"
	"github.com/kartFr/Asset-Reuploader/internal/atomicarray"
	"github.com/kartFr/Asset-Reuploader/internal/retry"
	"github.com/kartFr/Asset-Reuploader/internal/roblox/assetdelivery"
	"github.com/kartFr/Asset-Reuploader/internal/roblox/develop"
	"github.com/kartFr/Asset-Reuploader/internal/roblox/games"
	"github.com/kartFr/Asset-Reuploader/internal/roblox/ide"
	"github.com/kartFr/Asset-Reuploader/internal/shardedmap"
	"github.com/kartFr/Asset-Reuploader/internal/taskqueue"
)

const assetTypeID int32 = 24
const animationUploadRetryTries = 8
// SmoothQueue: even start spacing + concurrency cap (respect Roblox limits, don’t exceed).
const animationStartsPerMinute = 500
const animationMaxConcurrentUploads = 30

// Pause every N successful uploads while holding a concurrency slot (API breather).
const animationSuccessDrainEvery = 400
const animationSuccessDrainPause = 1200 * time.Millisecond

// When Retry-After is missing on 429 (rare); server hint via ide.RateLimitError otherwise.
const animationRateLimitMinBackoff = 600 * time.Millisecond
// Extra wait + pacer chill after any 429 (Retry-After is often a floor; API still hot).
const animationPost429ExtraWait = 1500 * time.Millisecond
const animationPost429Chill = 900 * time.Millisecond

// Parallel 50-id GetAssetsInfo chunks (metadata only).
const animationMaxParallelChunks = 8

var ErrUnauthorized = errors.New("authentication required to access asset")

func MoveValueToTop[T comparable](arr *atomicarray.AtomicArray[T], value T) {
	arr.Update(func(currentArray []T) []T {
		if currentArray[0] == value {
			return nil
		}

		for i, v := range currentArray {
			if v != value {
				continue
			}
			if i == 1 {
				currentArray[0], currentArray[1] = currentArray[1], currentArray[0]
				return currentArray
			}

			copy(currentArray[1:i+1], currentArray[0:i])
			currentArray[0] = value
			return currentArray
		}

		return nil
	})
}

func Reupload(ctx *context.Context, r *request.Request) {
	client := ctx.Client
	logger := ctx.Logger
	pauseController := ctx.PauseController
	resp := ctx.Response

	idsToUpload := len(r.IDs)
	var idsProcessed atomic.Int32

	defaultPlaceIDs := append([]int64(nil), r.DefaultPlaceIDs...)
	defaultPlaceIDsMap := make(map[int64]struct{}, len(defaultPlaceIDs))
	for _, placeID := range defaultPlaceIDs {
		defaultPlaceIDsMap[placeID] = struct{}{}
	}
	if r.PlaceID > 0 {
		if _, exists := defaultPlaceIDsMap[r.PlaceID]; !exists {
			defaultPlaceIDs = append(defaultPlaceIDs, r.PlaceID)
			defaultPlaceIDsMap[r.PlaceID] = struct{}{}
		}
	}

	var groupID int64
	if r.IsGroup {
		groupID = r.CreatorID
	}

	filter := assetutils.NewFilter(ctx, r, assetTypeID)

	creatorPlaceMap := shardedmap.New[*atomicarray.AtomicArray[int64]]()
	creatorMutexMap := shardedmap.New[*sync.RWMutex]()

	uploadQueue := taskqueue.NewSmoothQueue[int64](animationMaxConcurrentUploads, animationStartsPerMinute)
	var uploadSuccessCount atomic.Uint64

	// When any upload hits 429, briefly pause *all* in-flight uploads before their next
	// attempt so we don’t immediately hammer the API again from other goroutines.
	var quiesceMu sync.Mutex
	var quiesceUntil time.Time
	extendAPIQuiesce := func(d time.Duration) {
		if d < 500*time.Millisecond {
			d = 500 * time.Millisecond
		}
		if d > 12*time.Second {
			d = 12 * time.Second
		}
		quiesceMu.Lock()
		defer quiesceMu.Unlock()
		t := time.Now().Add(d)
		if t.After(quiesceUntil) {
			quiesceUntil = t
		}
	}
	waitAPIQuiesce := func() {
		quiesceMu.Lock()
		u := quiesceUntil
		quiesceMu.Unlock()
		if time.Now().Before(u) {
			time.Sleep(time.Until(u))
		}
	}

	groupGameQueue := taskqueue.New[*games.GamesResponse](time.Second*5, 8)
	userGameQueue := taskqueue.New[*games.GamesResponse](time.Second*5, 8)

	logger.Println("Reuploading animations...")

	newBatchError := func(amt int, m string, err any) {
		end := int(idsProcessed.Add(int32(amt)))
		start := end - amt
		logger.Error(uploaderror.NewBatch(start, end, idsToUpload, m, err))
	}

	newUploadError := func(m string, assetInfo *develop.AssetInfo, err any) {
		newValue := idsProcessed.Add(1)
		logger.Error(uploaderror.New(int(newValue), idsToUpload, m, assetInfo, err))
	}

	uploadAsset := func(wg *sync.WaitGroup, assetInfo *develop.AssetInfo, location string) {
		defer wg.Done()

		oldName := assetInfo.Name

		assetData, err := clientutils.GetRequest(client, location)
		if err != nil {
			newUploadError("Failed to get asset data", assetInfo, err)
			return
		}

		uploadHandler, err := ide.NewUploadAnimationHandler(client, assetInfo.Name, "", assetData, groupID)
		if err != nil {
			newUploadError("Failed to get upload handler", assetInfo, err)
			return
		}

		res := <-uploadQueue.QueueTask(func() (int64, error) {
			id, err := retry.Do(
				retry.NewOptions(
					retry.Tries(animationUploadRetryTries),
					retry.Delay(900*time.Millisecond),
				),
				func(try int) (int64, error) {
					pauseController.WaitIfPaused()
					waitAPIQuiesce()
					if try > 1 {
						uploadQueue.Limiter.Wait()
					}

					id, err := uploadHandler()
					if err == nil {
						return id, nil
					}

					switch err {
					case ide.UploadAnimationErrors.ErrNotLoggedIn:
						clientutils.GetNewCookie(ctx, r, "cookie expired")
					case ide.UploadAnimationErrors.ErrInappropriateName:
						assetInfo.Name = fmt.Sprintf("(%s) [Censored]", assetInfo.Name)
					default:
						if errors.Is(err, ide.ErrRateLimited) && try < animationUploadRetryTries {
							wait := animationRateLimitMinBackoff
							var rle *ide.RateLimitError
							if errors.As(err, &rle) && rle.RetryAfter > 0 {
								wait = rle.RetryAfter
							}
							wait += animationPost429ExtraWait
							const max429Wait = 60 * time.Second
							if wait > max429Wait {
								wait = max429Wait
							}
							uploadQueue.Chill(animationPost429Chill)
							uploadQueue.Limiter.Wait()
							time.Sleep(wait)
							// Let other concurrent uploads back off before their next poll/create.
							extend := wait / 4
							if extend < 2*time.Second {
								extend = 2 * time.Second
							}
							if extend > 8*time.Second {
								extend = 8 * time.Second
							}
							extendAPIQuiesce(extend)
						}
						switch err.(type) {
						case *net.OpError, *net.DNSError:
							uploadQueue.Limiter.Decrement()
						}
					}

					return 0, &retry.ContinueRetry{Err: err}
				},
			)
			if err != nil {
				return 0, err
			}
			if n := uploadSuccessCount.Add(1); n%uint64(animationSuccessDrainEvery) == 0 {
				time.Sleep(animationSuccessDrainPause)
			}
			return id, nil
		})

		if err := res.Error; err != nil {
			assetInfo.Name = oldName
			newUploadError("Failed to upload", assetInfo, err)
			return
		}

		newID := res.Result
		newValue := idsProcessed.Add(1)
		logger.Success(uploaderror.New(int(newValue), idsToUpload, "", assetInfo, newID))
		resp.AddItem(response.ResponseItem{
			OldID: assetInfo.ID,
			NewID: newID,
		})
	}

	getCreatorPlaceCache := func(creatorID int64, creatorType string) (*atomicarray.AtomicArray[int64], error) {
		creatorShard, exists := creatorPlaceMap.GetShard(creatorType)
		mutexShard, _ := creatorMutexMap.GetShard(creatorType)
		if !exists {
			creatorShard = creatorPlaceMap.NewShard(creatorType)
			mutexShard = creatorMutexMap.NewShard(creatorType)
		}

		if cache, cacheExists := creatorShard.Get(creatorID); cacheExists {
			return cache, nil
		}

		mutex, mutexExists := mutexShard.Get(creatorID)
		if !mutexExists {
			mutex = &sync.RWMutex{}
			mutexShard.Set(creatorID, mutex)
		}

		mutex.Lock()
		defer mutex.Unlock()

		if cache, cacheExists := creatorShard.Get(creatorID); cacheExists {
			return cache, nil
		}

		var resp *games.GamesResponse
		var err error
		if creatorType == "Group" {
			queueRes := <-groupGameQueue.QueueTask(func() (*games.GamesResponse, error) {
				return games.GroupGames(client, creatorID)
			})
			resp = queueRes.Result
			err = queueRes.Error
		} else {
			queueRes := <-userGameQueue.QueueTask(func() (*games.GamesResponse, error) {
				return games.UserGames(client, creatorID)
			})
			resp = queueRes.Result
			err = queueRes.Error
		}
		if err != nil {
			return nil, err
		}

		ids := make([]int64, 0, len(defaultPlaceIDs)) // we only do len defaultPlaceIds because there may be overlapping, i guess allocating more memory would be fine... idk guys im getting lazy just wait for revamp
		for _, placeInfo := range resp.Data {         // yes we copying many bytes per iteration, yes i dont care, yes this is another stupid message, yes code iwll get better on revamp :sob:
			rootPlaceID := placeInfo.RootPlace.ID

			if _, exists := defaultPlaceIDsMap[rootPlaceID]; exists {
				continue
			}
			ids = append(ids, rootPlaceID) // we no longer only need 1 valid place id :// ( ͡° ͜ʖ ͡°) yall remember this peak face lmk
		}
		ids = append(ids, defaultPlaceIDs...)

		cache := atomicarray.New(&ids)
		creatorShard.Set(creatorID, cache)
		mutexShard.Remove(creatorID)
		return cache, nil
	}

	getAssetLocations := func(body []*assetdelivery.AssetRequestItem, placeID int64) ([]*assetdelivery.AssetLocation, error) {
		runGetLocations := func(handler func() ([]*assetdelivery.AssetLocation, error)) ([]*assetdelivery.AssetLocation, error) {
			return retry.Do(
			retry.NewOptions(retry.Tries(6), retry.Delay(800*time.Millisecond)),
			func(try int) ([]*assetdelivery.AssetLocation, error) {
				pauseController.WaitIfPaused()

				locations, err := handler()
				if err != nil {
					return locations, &retry.ContinueRetry{Err: err}
				}

				for _, assetLocation := range locations {
					errs := assetLocation.Errors
					if errs == nil {
						continue
					}
					if errs[0].Message == "Authentication required to access Asset." {
						clientutils.GetNewCookie(ctx, r, "cookie expired")
						return locations, &retry.ContinueRetry{Err: ErrUnauthorized}
					}
				}

				return locations, nil
			},
		)
		}

		handlerWithPlace, err := assetdelivery.NewBatchHandler(client, body, placeID)
		if err != nil {
			return nil, err
		}
		locations, withPlaceErr := runGetLocations(handlerWithPlace)
		if withPlaceErr == nil {
			for _, assetLocation := range locations {
				if len(assetLocation.Locations) > 0 {
					return locations, nil
				}
			}
		}

		// Fallback path: some animations do not resolve with Roblox-Place-Id set.
		handlerWithoutPlace, err := assetdelivery.NewBatchHandler(client, body)
		if err != nil {
			if withPlaceErr != nil {
				return nil, withPlaceErr
			}
			return locations, nil
		}
		fallbackLocations, fallbackErr := runGetLocations(handlerWithoutPlace)
		if fallbackErr != nil {
			if withPlaceErr != nil {
				return nil, withPlaceErr
			}
			return nil, fallbackErr
		}
		return fallbackLocations, nil
	}

	batchUpload := func(wg *sync.WaitGroup, creatorID int64, creatorType string, creatorAssets []*develop.AssetInfo) {
		defer wg.Done()

		placeCache, err := getCreatorPlaceCache(creatorID, creatorType)
		if err != nil {
			newBatchError(len(creatorAssets), "Failed to get creator places", err)
			return
		}

		assetInfoMap := make(map[int64]*develop.AssetInfo)
		ids := make([]int64, len(creatorAssets))
		for i, assetInfo := range creatorAssets {
			ids[i] = assetInfo.ID
			assetInfoMap[assetInfo.ID] = assetInfo
		}
		body := assetutils.NewBatchBodyFromIDs(ids)

		var uploadWG sync.WaitGroup
		creatorPlaceCache := placeCache.Load()
		if len(creatorPlaceCache) == 0 {
			for _, req := range body {
				assetInfo := assetInfoMap[req.AssetID]
				newUploadError("Failed to get asset location", assetInfo, "no place IDs (add place(s) under Filter, or ensure the creator has a discoverable experience)")
			}
			return
		}

		lastLocationErrorByAssetID := make(map[int64]string)
		for _, placeID := range creatorPlaceCache {
			assetLocations, err := getAssetLocations(body, placeID)
			if err != nil {
				// Try another place instead of dropping the whole batch on one failed request.
				continue
			}
			for i, assetLocation := range assetLocations {
				if len(assetLocation.Locations) > 0 || i >= len(body) {
					continue
				}
				if errs := assetLocation.Errors; len(errs) > 0 {
					lastLocationErrorByAssetID[body[i].AssetID] = errs[0].Message
				}
			}

			var hadSuccess bool
			for assetIndex := len(assetLocations) - 1; assetIndex >= 0; assetIndex-- {
				assetLocation := assetLocations[assetIndex]
				if len(assetLocation.Locations) == 0 {
					continue
				}
				hadSuccess = true

				assetID := body[assetIndex].AssetID
				body = append(body[:assetIndex], body[assetIndex+1:]...)

				uploadWG.Add(1)
				go uploadAsset(&uploadWG, assetInfoMap[assetID], assetLocation.Locations[0].Location)
			}
			if hadSuccess && len(creatorPlaceCache) > 1 {
				MoveValueToTop(placeCache, placeID)
			}
			if len(body) == 0 {
				break
			}
		}

		// Anything left in body got no URL from any place. Do not index into the last
		// batch response here: after partial successes, body indices no longer match.
		for _, req := range body {
			assetInfo := assetInfoMap[req.AssetID]
			if lastErr, hasSpecificErr := lastLocationErrorByAssetID[req.AssetID]; hasSpecificErr && lastErr != "" {
				newUploadError("Failed to get asset location", assetInfo, lastErr)
				continue
			}
			newUploadError("Failed to get asset location", assetInfo, "no download URL from any place (tried creator places and fallback without placeId)")
		}

		uploadWG.Wait()
	}

	batchProcess := func(wg *sync.WaitGroup, res assetutils.AssetsInfoResult, batchSize int) {
		defer wg.Done()
		assetsInfo := res.Result

		if err := res.Error; err != nil {
			newBatchError(batchSize, "Failed to get assets info", err)
			return
		}

		filteredInfo := filter(assetsInfo)
		filteredInfoLength := len(filteredInfo)
		idsProcessed.Add(int32(batchSize - filteredInfoLength))
		if filteredInfoLength == 0 {
			return
		}

		CreatorAssets := make(map[string]map[int64][]*develop.AssetInfo)
		for _, assetInfo := range filteredInfo {
			assetCreatorType := assetInfo.Creator.Type
			assetCreatorID := assetInfo.Creator.TargetID

			creatorType, exists := CreatorAssets[assetCreatorType]
			if !exists {
				creatorType = make(map[int64][]*develop.AssetInfo)
				CreatorAssets[assetCreatorType] = creatorType
			}

			creatorAssets, exists := creatorType[assetCreatorID]
			if !exists {
				creatorAssets = make([]*develop.AssetInfo, 0)
				creatorType[assetCreatorID] = creatorAssets
			}

			creatorType[assetCreatorID] = append(creatorAssets, assetInfo)
		}

		var uploadWG sync.WaitGroup
		for creatorType, creatorAssetMap := range CreatorAssets {
			uploadWG.Add(len(creatorAssetMap))

			for creatorID, creatorAssets := range creatorAssetMap {
				go batchUpload(&uploadWG, creatorID, creatorType, creatorAssets)
			}
		}
		uploadWG.Wait()
	}

	var wg sync.WaitGroup
	tasks := assetutils.GetAssetsInfoInChunks(ctx, r)
	wg.Add(len(tasks))
	chunkSlots := make(chan struct{}, animationMaxParallelChunks)
	for i, task := range tasks {
		batchSize := 50
		if i == len(tasks)-1 {
			batchSize = idsToUpload % 50
			if batchSize == 0 {
				batchSize = 50
			}
		}

		chunkSlots <- struct{}{}
		go func(taskCh <-chan assetutils.AssetsInfoResult, bs int) {
			defer func() { <-chunkSlots }()
			batchProcess(&wg, <-taskCh, bs)
		}(task, batchSize)
	}
	wg.Wait()
}
