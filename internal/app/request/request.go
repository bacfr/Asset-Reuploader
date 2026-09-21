package request

import (
	"fmt"
	"github.com/kartFr/Asset-Reuploader/internal/roblox"
	"github.com/kartFr/Asset-Reuploader/internal/roblox/games"
)

type RawRequest struct {
	PlaceID              int64   `json:"placeId"`
	CreatorID            int64   `json:"creatorId"`
	IDs                  []int64 `json:"ids"`
	DefaultPlaceIDs      []int64 `json:"defaultPlaceIds"`
	PluginVersion        string  `json:"pluginVersion"`
	AssetType            string  `json:"assetType"`
	ExportJSON           bool    `json:"exportJSON"`
	IsGroup              bool    `json:"isGroup"`
	BannedUserID         int64   `json:"bannedUserId,omitempty"`
	FetchAllGroupGames   bool    `json:"fetchAllGroupGames,omitempty"`
	FetchAllBannedGames  bool    `json:"fetchAllBannedGames,omitempty"`
}

type Request struct {
	UniverseID         int64
	PlaceID            int64
	CreatorID          int64
	IDs                []int64
	DefaultPlaceIDs    []int64
	IsGroup            bool
	BannedUserID       int64
	FetchAllGroupGames bool
	FetchAllBannedGames bool
}

func FromRawRequest(c *roblox.Client, req *RawRequest) (*Request, error) {
	placeID := req.PlaceID

	// If fetching from banned user, first get their games
	if req.BannedUserID > 0 && req.FetchAllBannedGames {
		bannedGames, err := games.GetAllUserGamesWithPagination(c, req.BannedUserID)
		if err != nil {
			return nil, fmt.Errorf("failed to fetch banned user games: %w", err)
		}

		// Collect all root place IDs from banned user's games
		for _, gamePage := range bannedGames {
			for _, game := range gamePage.Data {
				rootPlaceID := game.RootPlace.ID
				if rootPlaceID > 0 {
					req.DefaultPlaceIDs = append(req.DefaultPlaceIDs, rootPlaceID)
				}
			}
		}
	}

	// If fetching all group games with pagination
	if req.IsGroup && req.FetchAllGroupGames && req.CreatorID > 0 {
		groupGames, err := games.GetAllGroupGamesWithPagination(c, req.CreatorID)
		if err != nil {
			return nil, fmt.Errorf("failed to fetch group games: %w", err)
		}

		// Collect all root place IDs from group's games
		for _, gamePage := range groupGames {
			for _, game := range gamePage.Data {
				rootPlaceID := game.RootPlace.ID
				if rootPlaceID > 0 {
					req.DefaultPlaceIDs = append(req.DefaultPlaceIDs, rootPlaceID)
				}
			}
		}
	}

	// Get place details for the main place ID if provided
	var universeID int64
	if placeID > 0 {
		placesInfo, err := games.MultiGetPlaceDetails(c, []int64{placeID})
		if err != nil {
			return nil, err
		}
		if len(placesInfo) > 0 {
			universeID = placesInfo[0].UniverseID
		}
	}

	return &Request{
		UniverseID:         universeID,
		PlaceID:            placeID,
		CreatorID:          req.CreatorID,
		IDs:                req.IDs,
		DefaultPlaceIDs:    req.DefaultPlaceIDs,
		IsGroup:            req.IsGroup,
		BannedUserID:       req.BannedUserID,
		FetchAllGroupGames: req.FetchAllGroupGames,
		FetchAllBannedGames: req.FetchAllBannedGames,
	}, nil
}
