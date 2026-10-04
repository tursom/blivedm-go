package api

import (
	"context"
	"errors"
	"net/http"
	"net/url"
	"strconv"
	"strings"
)

type SilentUserResponse struct {
	Success bool   `json:"success"`
	Code    int64  `json:"code"`
	Message string `json:"message"`
}

type ShieldKeywordListResponse struct {
	Code    int64                 `json:"code"`
	Message string                `json:"message"`
	Msg     string                `json:"msg"`
	Data    ShieldKeywordListData `json:"data"`
}

type ShieldKeywordListData struct {
	KeywordList []ShieldKeyword `json:"keyword_list"`
	MaxLimit    int             `json:"max_limit"`
}

type ShieldKeyword struct {
	Keyword  string `json:"keyword"`
	Uid      int64  `json:"uid"`
	Name     string `json:"name"`
	IsAnchor int    `json:"is_anchor"`
}

func GetShieldKeywordList(roomID int, cookie string) (*ShieldKeywordListResponse, error) {
	csrf, ok := ExtractCookieValue(cookie, "bili_jct")
	if !ok {
		return nil, errors.New("missing bili_jct")
	}

	form := url.Values{
		"room_id":    {strconv.Itoa(roomID)},
		"csrf_token": {csrf},
		"csrf":       {csrf},
		"visit_id":   {""},
	}

	result := &ShieldKeywordListResponse{}
	err := postLiveForm(
		"https://api.live.bilibili.com/xlive/web-ucenter/v1/banned/GetShieldKeywordList",
		cookie,
		"https://live.bilibili.com/",
		form,
		result,
	)
	if err != nil {
		return nil, err
	}
	return result, nil
}

func AddShieldKeyword(roomID int, keyword, cookie string) (*SilentUserResponse, error) {
	csrf, ok := ExtractCookieValue(cookie, "bili_jct")
	if !ok {
		return nil, errors.New("missing bili_jct")
	}

	form := url.Values{
		"room_id":    {strconv.Itoa(roomID)},
		"keyword":    {keyword},
		"csrf_token": {csrf},
		"csrf":       {csrf},
	}

	return postSilentUserResponse(
		"https://api.live.bilibili.com/xlive/web-ucenter/v1/banned/AddShieldKeyword",
		cookie,
		"https://link.bilibili.com/",
		form,
	)
}

func DelShieldKeyword(roomID int, keyword, cookie string) (*SilentUserResponse, error) {
	csrf, ok := ExtractCookieValue(cookie, "bili_jct")
	if !ok {
		return nil, errors.New("missing bili_jct")
	}

	form := url.Values{
		"room_id":    {strconv.Itoa(roomID)},
		"keyword":    {keyword},
		"csrf_token": {csrf},
		"csrf":       {csrf},
	}

	return postSilentUserResponse(
		"https://api.live.bilibili.com/xlive/web-ucenter/v1/banned/DelShieldKeyword",
		cookie,
		"https://link.bilibili.com/",
		form,
	)
}

func AddSilentUser(roomID, tuid int, cookie string, silentType, hour int, msg string) (*SilentUserResponse, error) {
	csrf, ok := ExtractCookieValue(cookie, "bili_jct")
	if !ok {
		return nil, errors.New("missing bili_jct")
	}

	form := url.Values{
		"room_id":    {strconv.Itoa(roomID)},
		"tuid":       {strconv.Itoa(tuid)},
		"msg":        {msg},
		"mobile_app": {"web"},
		"type":       {strconv.Itoa(silentType)},
		"hour":       {strconv.Itoa(hour)},
		"csrf_token": {csrf},
		"csrf":       {csrf},
		"visit_id":   {""},
	}

	return postSilentUserResponse(
		"https://api.live.bilibili.com/xlive/web-ucenter/v1/banned/AddSilentUser",
		cookie,
		"https://live.bilibili.com/"+strconv.Itoa(roomID),
		form,
	)
}

func postSilentUserResponse(endpoint, cookie, referer string, form url.Values) (*SilentUserResponse, error) {
	result := struct {
		Code    int64  `json:"code"`
		Message string `json:"message"`
		Msg     string `json:"msg"`
	}{}

	if err := postLiveForm(endpoint, cookie, referer, form, &result); err != nil {
		return nil, err
	}
	message := result.Message
	if message == "" {
		message = result.Msg
	}
	return &SilentUserResponse{
		Success: result.Code == 0,
		Code:    result.Code,
		Message: message,
	}, nil
}

func postLiveForm(endpoint, cookie, referer string, form url.Values, result any) error {
	req, err := http.NewRequestWithContext(context.Background(), http.MethodPost, endpoint, strings.NewReader(form.Encode()))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("User-Agent", userAgent)
	req.Header.Set("Referer", referer)
	if cookie != "" {
		req.Header.Set("Cookie", cookie)
	}

	return decodeResponse(req, result)
}
