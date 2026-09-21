package games

import "time"

type GamesResponse struct {
	PreviousPageCursor string `json:"previousPageCursor"`
	NextPageCursor     string `json:"nextPageCursor"`
	Data               []struct {
		ID            int64  `json:"id"`
		Name          string `json:"name"`
		Description   string `json:"description"`
		Creator       struct {
			ID   int64  `json:"id"`
			Type string `json:"type"`
		} `json:"creator"`
		RootPlace struct {
			ID   int64  `json:"id"`
			Type string `json:"type"`
		} `json:"rootPlace"`
		Created       time.Time `json:"created"`
		Updated       time.Time `json:"updated"`
		PlaceVisits   int64     `json:"placeVisits"`
		IsPublic      bool      `json:"isPublic"`
		IsFriendsOnly bool      `json:"isFriendsOnly"`
	} `json:"data"`
}
