package games

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"time"

	"github.com/kartFr/Asset-Reuploader/internal/retry"
	"github.com/kartFr/Asset-Reuploader/internal/roblox"
)

// BannedUserGamesResponse represents the response from getting games of a banned user
type BannedUserGamesResponse struct {
	PreviousPageCursor string `json:"previousPageCursor"`
	NextPageCursor     string `json:"nextPageCursor"`
	Data               []struct {
		ID          int64  `json:"id"`
		Name        string `json:"name"`
		Description string `json:"description"`
		Creator     struct {
			ID   int64  `json:"id"`
			Type string `json:"type"`
		} `json:"creator"`
		RootPlace struct {
			ID   int64  `json:"id"`
			Type string `json:"type"`
		} `json:"rootPlace"`
		Created     time.Time `json:"created"`
		Updated     time.Time `json:"updated"`
		PlaceVisits int64     `json:"placeVisits"`
		IsPublic    bool      `json:"isPublic"`
		IsFriendsOnly bool   `json:"isFriendsOnly"`
	} `json:"data"`
}

// NewBannedUserGamesHandler creates a handler to fetch games from a banned user
// This uses the same endpoint as regular user games but is optimized for banned users
func NewBannedUserGamesHandler(c *roblox.Client, userID int64) (func() (*BannedUserGamesResponse, error), error) {
	url := fmt.Sprintf("https://games.roblox.com/v2/users/%d/games?limit=50", userID)
	req, err := http.NewRequest("GET", url, http.NoBody)
	if err != nil {
		return func() (*BannedUserGamesResponse, error) { return nil, nil }, err
	}

	return func() (*BannedUserGamesResponse, error) {
		req.AddCookie(&http.Cookie{
			Name:  ".ROBLOSECURITY",
			Value: c.Cookie,
		})

		resp, err := c.DoRequest(req)
		if err != nil {
			return nil, err
		}
		defer resp.Body.Close()

		if resp.StatusCode != http.StatusOK {
			return nil, errors.New(resp.Status)
		}

		var gamesResponse BannedUserGamesResponse
		json.NewDecoder(resp.Body).Decode(&gamesResponse)
		return &gamesResponse, nil
	}, nil
}

// BannedUserGames fetches all games from a banned user with pagination support
func BannedUserGames(c *roblox.Client, userID int64) (*BannedUserGamesResponse, error) {
	handler, err := NewBannedUserGamesHandler(c, userID)
	if err != nil {
		return nil, err
	}

	return retry.Do(
		retry.NewOptions(retry.Tries(5), retry.Delay(500*time.Millisecond)),
		func(_ int) (*BannedUserGamesResponse, error) {
			games, err := handler()
			if err != nil {
				return nil, &retry.ContinueRetry{Err: err}
			}

			return games, nil
		},
	)
}

// GetAllUserGamesWithPagination fetches all games from a user by following pagination cursors
func GetAllUserGamesWithPagination(c *roblox.Client, userID int64) ([]GamesResponse, error) {
	allGames := make([]GamesResponse, 0)
	cursor := ""

	for {
		url := fmt.Sprintf("https://games.roblox.com/v2/users/%d/games?limit=50", userID)
		if cursor != "" {
			url += "&cursor=" + cursor
		}

		req, err := http.NewRequest("GET", url, http.NoBody)
		if err != nil {
			return allGames, err
		}

		req.AddCookie(&http.Cookie{
			Name:  ".ROBLOSECURITY",
			Value: c.Cookie,
		})

		resp, err := c.DoRequest(req)
		if err != nil {
			return allGames, &retry.ContinueRetry{Err: err}
		}

		var gamesResp GamesResponse
		json.NewDecoder(resp.Body).Decode(&gamesResp)
		resp.Body.Close()

		if len(gamesResp.Data) > 0 {
			allGames = append(allGames, gamesResp)
		}

		if gamesResp.NextPageCursor == "" || len(gamesResp.Data) < 50 {
			break
		}

		cursor = gamesResp.NextPageCursor
		time.Sleep(200 * time.Millisecond) // Rate limiting between pages
	}

	return allGames, nil
}

// GetAllGroupGamesWithPagination fetches all games from a group by following pagination cursors
func GetAllGroupGamesWithPagination(c *roblox.Client, groupID int64) ([]GamesResponse, error) {
	allGames := make([]GamesResponse, 0)
	cursor := ""

	for {
		url := fmt.Sprintf("https://games.roblox.com/v2/groups/%d/gamesV2?limit=100", groupID)
		if cursor != "" {
			url += "&cursor=" + cursor
		}

		req, err := http.NewRequest("GET", url, http.NoBody)
		if err != nil {
			return allGames, err
		}

		req.AddCookie(&http.Cookie{
			Name:  ".ROBLOSECURITY",
			Value: c.Cookie,
		})

		resp, err := c.DoRequest(req)
		if err != nil {
			return allGames, &retry.ContinueRetry{Err: err}
		}

		var gamesResp GamesResponse
		json.NewDecoder(resp.Body).Decode(&gamesResp)
		resp.Body.Close()

		if len(gamesResp.Data) > 0 {
			allGames = append(allGames, gamesResp)
		}

		if gamesResp.NextPageCursor == "" || len(gamesResp.Data) < 100 {
			break
		}

		cursor = gamesResp.NextPageCursor
		time.Sleep(200 * time.Millisecond) // Rate limiting between pages
	}

	return allGames, nil
}
