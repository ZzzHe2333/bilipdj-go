package live

import "testing"

func TestDouyinLiveIDSupportsCopiedShareText(t *testing.T) {
    cases := map[string]string{
        "123456": "123456",
        "https://live.douyin.com/123456?is_from_webapp=1": "123456",
        "分享直播间 复制链接 https://live.douyin.com/123456?enter_from=share 快来": "123456",
    }
    for input, want := range cases {
        got, err := ValidateDouyinRoom(input)
        if err != nil || got != want { t.Errorf("%q -> %q / %v, want %q", input, got, err, want) }
    }
    for _, invalid := range []string{"https://live.douyin.com.evil.example/123", "https://evil.example/123", "javascript:alert(1)", "https://live.douyin.com:9999/123"} {
        if _, err := ValidateDouyinRoom(invalid); err == nil { t.Errorf("unsafe input accepted: %q", invalid) }
    }
}
