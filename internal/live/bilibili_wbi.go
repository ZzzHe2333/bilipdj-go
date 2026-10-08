package live

import (
	"context"
	"crypto/md5"
	"encoding/hex"
	"fmt"
	"net/url"
	"path"
	"strconv"
	"strings"
	"time"
)

// See Bilibili WBI signing convention: the permutation and URL canonicalization
// apply to getDanmuInfo when the browser API requires a signed request.
var wbiTable = []int{46, 47, 18, 2, 53, 8, 23, 32, 15, 50, 10, 31, 58, 3, 45, 35, 27, 43, 5, 49, 33, 9, 42, 19, 29, 28, 14, 39, 12, 38, 41, 13, 37, 48, 7, 16, 24, 55, 40, 61, 26, 17, 0, 1, 60, 51, 30, 4, 22, 25, 54, 21, 56, 59, 6, 63, 57, 62, 11, 36, 20, 34, 44, 52}

type biliNav struct {
	Code int `json:"code"`
	Data struct {
		Mid     int64 `json:"mid"`
		IsLogin bool  `json:"isLogin"`
		WbiImg  struct {
			ImgURL string `json:"img_url"`
			SubURL string `json:"sub_url"`
		} `json:"wbi_img"`
	} `json:"data"`
}
type biliSPI struct {
	Code int `json:"code"`
	Data struct {
		Buvid3 string `json:"b_3"`
	} `json:"data"`
}

func biliCookieValue(cookie, key string) string {
	for _, p := range strings.Split(cookie, ";") {
		k, v, ok := strings.Cut(strings.TrimSpace(p), "=")
		if ok && strings.EqualFold(k, key) {
			return v
		}
	}
	return ""
}
func wbiKey(img, sub string) (string, error) {
	a := strings.TrimSuffix(path.Base(strings.Split(img, "?")[0]), path.Ext(strings.Split(img, "?")[0]))
	b := strings.TrimSuffix(path.Base(strings.Split(sub, "?")[0]), path.Ext(strings.Split(sub, "?")[0]))
	raw := a + b
	if len(raw) < 64 {
		return "", fmt.Errorf("WBI 图片密钥长度错误")
	}
	var key strings.Builder
	for _, p := range wbiTable {
		key.WriteByte(raw[p])
	}
	return key.String()[:32], nil
}
func wbiSign(params url.Values, key string, at time.Time) url.Values {
	v := url.Values{}
	for name, values := range params {
		if len(values) > 0 {
			v.Set(name, strings.Map(func(r rune) rune {
				if strings.ContainsRune("!'()*", r) {
					return -1
				}
				return r
			}, values[0]))
		}
	}
	v.Set("wts", strconv.FormatInt(at.Unix(), 10))
	digest := md5.Sum([]byte(v.Encode() + key))
	v.Set("w_rid", hex.EncodeToString(digest[:]))
	return v
}
func (b Bilibili) signedDanmuURL(ctx context.Context, room int64, cookie string) (string, biliNav, error) {
	var nav biliNav
	err := biliGet(ctx, b.client(), "https://api.bilibili.com/x/web-interface/nav", cookie, &nav)
	if err != nil {
		return "", nav, fmt.Errorf("WBI 导航数据失败: %w", err)
	}
	if nav.Code != 0 {
		return "", nav, fmt.Errorf("B站登录导航响应 code=%d", nav.Code)
	}
	key, err := wbiKey(nav.Data.WbiImg.ImgURL, nav.Data.WbiImg.SubURL)
	if err != nil {
		return "", nav, err
	}
	params := url.Values{"id": {strconv.FormatInt(room, 10)}, "type": {"0"}, "web_location": {"444.8"}}
	return fmt.Sprintf("%s/xlive/web-room/v1/index/getDanmuInfo?%s", biliAPI, wbiSign(params, key, time.Now()).Encode()), nav, nil
}
func (b Bilibili) discoveryCookie(ctx context.Context, original string) string {
	if biliCookieValue(original, "buvid3") != "" {
		return original
	}
	var spi biliSPI
	if biliGet(ctx, b.client(), "https://api.bilibili.com/x/frontend/finger/spi", original, &spi) == nil && spi.Code == 0 && spi.Data.Buvid3 != "" {
		if original != "" {
			return original + "; buvid3=" + spi.Data.Buvid3
		}
		return "buvid3=" + spi.Data.Buvid3
	}
	return original
}
