// Package httplib
// @Title  title
// @Description  desc
// @Author  yr  2024/11/20
// @Update  yr  2024/11/20
package httplib

import (
	"encoding/json"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/njtc406/emberengine/engine/pkg/def"
)

func CheckUrl(u string) string {
	if strings.Contains(u, `http://`) || strings.Contains(u, `https://`) {
		return u
	} else {
		return `http://` + u
	}
}

func Request(method, addr, api string, body interface{}, resData interface{}) error {
	removeUrl := CheckUrl(addr) + api

	var bodyReader io.Reader
	if body != nil {
		bodyBytes, err := json.Marshal(body)
		if err != nil {
			return def.ErrJsonMarshalFailed
		}
		bodyReader = strings.NewReader(string(bodyBytes))
	}
	client := &http.Client{
		Timeout: time.Second * 3,
	}
	req, err := http.NewRequest(method, removeUrl, bodyReader)
	if err != nil {
		return def.ErrHttpCreateRequestFailed
	}

	res, err := client.Do(req)
	if err != nil {
		return def.ErrHttpRequestFailed
	}

	// 获取数据
	defer res.Body.Close()

	resBody, err := io.ReadAll(res.Body)
	if err != nil {
		return def.ErrHttpReadResponseFailed
	}

	if err = json.Unmarshal(resBody, &resData); err != nil {
		return def.ErrJsonUnmarshalFailed
	}

	return nil
}
