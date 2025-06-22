// Package httplib
// @Title  title
// @Description  desc
// @Author  yr  2024/11/20
// @Update  yr  2024/11/20
package httplib

import (
	"encoding/json"
	"fmt"
	"github.com/njtc406/emberengine/engine/pkg/def"
	"io"
	"net/http"
	"strings"
	"time"
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

	//log.SysLogger.Debugf("-->req url: %v", removeUrl)
	fmt.Println("-->req url: ", removeUrl)

	var bodyReader io.Reader
	if body != nil {
		bodyBytes, err := json.Marshal(body)
		if err != nil {
			//log.SysLogger.Errorf("json marshal failed: %v", err)
			fmt.Println("json marshal failed:", err)
			return def.ErrJsonMarshalFailed
		}
		bodyReader = strings.NewReader(string(bodyBytes))
	}
	client := &http.Client{
		Timeout: time.Second * 3,
	}
	req, err := http.NewRequest(method, removeUrl, bodyReader)
	if err != nil {
		//log.SysLogger.Errorf("http create request failed: %v", err)
		fmt.Println("http create request failed:", err)
		return def.ErrHttpCreateRequestFailed
	}

	res, err := client.Do(req)
	if err != nil {
		//log.SysLogger.Errorf("http request failed: %v", err)
		fmt.Println("http request failed:", err)
		return def.ErrHttpRequestFailed
	}

	// 获取数据
	defer res.Body.Close()

	resBody, err := io.ReadAll(res.Body)
	if err != nil {
		//log.SysLogger.Errorf("read response body failed: %v", err)
		fmt.Println("read response body failed:", err)
		return def.ErrHttpReadResponseFailed
	}

	if err = json.Unmarshal(resBody, &resData); err != nil {
		//log.SysLogger.Errorf("json unmarshal failed: %v", err)
		fmt.Println("json unmarshal failed:", err)
		return def.ErrJsonUnmarshalFailed
	}

	//log.SysLogger.Debugf("resData %+v", resData)
	fmt.Println("resData:", resData)

	return nil
}
