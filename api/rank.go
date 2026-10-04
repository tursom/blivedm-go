package api

import (
	"context"
	"fmt"
)

type ContributionRankResponse struct {
	Code    int                  `json:"code"`
	Message string               `json:"message"`
	Msg     string               `json:"msg"`
	Data    ContributionRankData `json:"data"`
}

type ContributionRankData struct {
	Count int                    `json:"count"`
	Item  []ContributionRankUser `json:"item"`
}

type ContributionRankUser struct {
	Uid        int64                  `json:"uid"`
	Name       string                 `json:"name"`
	Face       string                 `json:"face"`
	Rank       int                    `json:"rank"`
	Score      int64                  `json:"score"`
	GuardLevel int                    `json:"guard_level"`
	MedalInfo  *ContributionMedalInfo `json:"medal_info"`
}

type ContributionMedalInfo struct {
	MedalName       string `json:"medal_name"`
	Level           int    `json:"level"`
	MedalColorStart int    `json:"medal_color_start"`
}

func (m *ContributionMedalInfo) MedalColor() string {
	if m == nil {
		return ""
	}
	return fmt.Sprintf("#%06x", m.MedalColorStart)
}

func GetContributionRank(roomID, ruid int, cookie string, page, pageSize int) (*ContributionRankResponse, error) {
	return GetContributionRankContext(context.Background(), roomID, ruid, cookie, page, pageSize)
}

func GetContributionRankContext(ctx context.Context, roomID, ruid int, cookie string, page, pageSize int) (*ContributionRankResponse, error) {
	if page <= 0 {
		page = 1
	}
	if pageSize <= 0 {
		pageSize = 100
	}

	rawURL := fmt.Sprintf(
		"https://api.live.bilibili.com/xlive/general-interface/v1/rank/queryContributionRank?ruid=%d&room_id=%d&page=%d&page_size=%d&type=online_rank&switch=contribution_rank&platform=web&web_location=0.0",
		ruid, roomID, page, pageSize,
	)
	signedURL, err := WbiKeysSignStringContext(ctx, rawURL)
	if err != nil {
		return nil, err
	}

	result := &ContributionRankResponse{}
	headers := liveHeaders(cookie)
	headers.Set("Referer", "https://live.bilibili.com/")
	if err := GetJsonWithHeaderContext(ctx, signedURL, headers, result); err != nil {
		return nil, err
	}
	return result, nil
}
