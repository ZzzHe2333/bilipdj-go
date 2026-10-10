package update

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"runtime"
	"strconv"
	"strings"
	"time"
)

// Proxy endpoints forward only fixed GitHub Release URLs from this repo.
// These community nodes are untrusted transport, NOT a cryptographic signer.
// Keep the historical constants for existing clients and tests.
const (
 ProxyPrefix = "https://gh-proxy.com/"
 ProxyAlternate = "https://gh-proxy.org/"
 ProxyAkams = "https://github.akams.cn/"
 ProxyGHFile = "https://ghfile.geekertao.top/"
 ProxyGitHubDPik = "https://github.dpik.top/"
 ProxyGHDPik = "https://gh.dpik.top/"
 maxPackage = 250 << 20
)

type proxyEndpoint struct { name, prefix string }

var communityProxies = []proxyEndpoint{
 {"gh-proxy-com", ProxyPrefix},
 {"gh-proxy-org", ProxyAlternate},
 {"akams", ProxyAkams},
 {"ghfile", ProxyGHFile},
 {"github-dpik", ProxyGitHubDPik},
 {"gh-dpik", ProxyGHDPik},
}

// Automatic and accelerated modes try every predefined node; choosing a
// specific service limits routing to only that provider.
func proxiesForSource(source string) []proxyEndpoint {
 if source == "auto" || source == "accelerated" { return communityProxies }
 for _, p := range communityProxies { if p.name == source { return []proxyEndpoint{p} } }
 return nil
}
func validUpdateSource(source string) bool {
 return source == "auto" || source == "official" || source == "accelerated" || len(proxiesForSource(source)) == 1
}
func sourceCandidates(u, source string, apiOverride bool) []string {
 if apiOverride || source == "official" { return []string{u} }
 candidates := make([]string, 0, len(communityProxies)+1)
 if source == "auto" { candidates = append(candidates, u) }
 for _, p := range proxiesForSource(source) { candidates = append(candidates, p.prefix+u) }
 if source == "accelerated" { candidates = append(candidates, u) }
 return candidates
}

type Release struct {
	Version         string `json:"version"`
	Current         string `json:"current"`
	UpdateAvailable bool   `json:"update_available"`
	Asset           string `json:"asset"`
	URL             string `json:"url"`
	Notes           string `json:"notes"`
	HasSHA256       bool   `json:"has_sha256"`
	Size            int64  `json:"size"`
	Source          string `json:"source"`
}
// DownloadProgress reports bytes from the actual streaming response, not
// guessed elapsed time. A missing size is not shown as 100%.
type DownloadProgress struct {
 Phase string `json:"phase"`
 Version string `json:"version,omitempty"`
 Source string `json:"source,omitempty"`
 DownloadedBytes int64 `json:"downloaded_bytes"`
 TotalBytes int64 `json:"total_bytes"`
 Percent float64 `json:"percent"`
 SpeedBPS float64 `json:"speed_bps"`
 Message string `json:"message,omitempty"`
}
type VersionChoice struct {
 Version string `json:"version"`
 Size int64 `json:"size"`
 Current bool `json:"current"`
 IsOlder bool `json:"is_older"`
 HasSHA256 bool `json:"has_sha256"`
}
type Downloaded struct {
	File    string `json:"file"`
	SHA256  string `json:"sha256"`
	Version string `json:"version"`
	Message string `json:"message"`
}
type Service struct {
	Repo, Version, Dir string
	APIBase            string // httptest-only compatibility
	Client             *http.Client
}

func New(repo, version, dir string) *Service {
	return &Service{Repo: repo, Version: version, Dir: dir, Client: &http.Client{Timeout: 65 * time.Second}}
}

type ghAsset struct {
	Name string `json:"name"`
	URL  string `json:"browser_download_url"`
	Size int64  `json:"size"`
	SHA  string `json:"sha256"`
}
type ghRelease struct {
	Tag    string    `json:"tag_name"`
	Body   string    `json:"body"`
	Assets []ghAsset `json:"assets"`
	Source string    `json:"-"`
}
type releaseManifest struct {
	Version  string `json:"version"`
	Tag      string `json:"tag_name"`
	Notes    string `json:"notes"`
	Packages map[string]struct {
		Name string `json:"name"`
		Size int64  `json:"size"`
		SHA  string `json:"sha256"`
	} `json:"packages"`
}

func (s *Service) filename() string {
	return fmt.Sprintf("bilipdj-go-%s-%s.zip", runtime.GOOS, runtime.GOARCH)
}
func (s *Service) latestURL() string {
	if s.APIBase != "" {
		return strings.TrimRight(s.APIBase, "/") + "/releases/latest"
	}
	return "https://api.github.com/repos/" + s.Repo + "/releases/latest"
}
func (s *Service) manifestURL() string {
	return "https://github.com/" + s.Repo + "/releases/latest/download/update-manifest.json"
}
func (s *Service) client() *http.Client {
	if s.Client != nil {
		return s.Client
	}
	return &http.Client{Timeout: 65 * time.Second}
}
func (s *Service) fetch(ctx context.Context, u string, max int64) ([]byte, error) {
	req, err := http.NewRequestWithContext(ctx, "GET", u, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("User-Agent", "bilipdj-go-updater/1.0")
	resp, err := s.client().Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		return nil, fmt.Errorf("HTTP %d", resp.StatusCode)
	}
	data, err := io.ReadAll(io.LimitReader(resp.Body, max+1))
	if err != nil {
		return nil, err
	}
	if int64(len(data)) > max {
		return nil, errors.New("下载超过体积上限")
	}
	return data, nil
}
func (s *Service) fetchJSON(ctx context.Context, u string, dest any) error {
	b, e := s.fetch(ctx, u, 1<<20)
	if e != nil {
		return e
	}
	return json.Unmarshal(b, dest)
}
func validateRepo(r string) bool {
	return regexp.MustCompile(`^[A-Za-z0-9_.-]+/[A-Za-z0-9_.-]+$`).MatchString(r)
}
func (s *Service) releaseAssetURL(tag, name string) string {
	return "https://github.com/" + s.Repo + "/releases/download/" + url.PathEscape(tag) + "/" + url.PathEscape(name)
}
func (s *Service) fromManifest(ctx context.Context, source string) (ghRelease, error) {
 var last error
 for _, proxy := range proxiesForSource(source) {
  var manifest releaseManifest
  c, cancel := context.WithTimeout(ctx, 4*time.Second)
  err := s.fetchJSON(c, proxy.prefix+s.manifestURL(), &manifest)
  cancel()
  if err != nil { last = err; continue }
  if !regexp.MustCompile(`^v[0-9]+\.[0-9]+\.[0-9]+$`).MatchString(manifest.Tag) || "v"+manifest.Version != manifest.Tag {
   last = errors.New("加速更新清单版本号不合法")
   continue
  }
  r := ghRelease{Tag: manifest.Tag, Body: manifest.Notes, Source: "accelerated"}
  if source != "auto" && source != "accelerated" { r.Source = source }
  for name, item := range manifest.Packages {
   if !regexp.MustCompile(`^bilipdj-go-(windows|linux|darwin)-(amd64|arm64)\.zip$`).MatchString(name) ||
      item.Name != name || item.Size < 1 || item.Size > maxPackage || !validHash(item.SHA) { continue }
   r.Assets = append(r.Assets, ghAsset{Name:name, URL:s.releaseAssetURL(r.Tag,name), Size:item.Size, SHA:strings.ToLower(item.SHA)})
  }
  if len(r.Assets) == 0 { last=errors.New("加速更新清单没有有效下载包"); continue }
  return r, nil
 }
 if last == nil { last=errors.New("没有可用的公益加速节点") }
 return ghRelease{}, fmt.Errorf("GitHub 代理更新清单不可用：%w", last)
}

func (s *Service) latest(ctx context.Context, source string) (ghRelease, ghAsset, ghAsset, error) {
	if !validateRepo(s.Repo) {
		return ghRelease{}, ghAsset{}, ghAsset{}, errors.New("无效 GitHub 仓库名")
	}
	if !validUpdateSource(source) {
  return ghRelease{}, ghAsset{}, ghAsset{}, errors.New("无效的下载线路")
 }
	var r ghRelease
	var err error
	if source != "auto" && source != "official" && s.APIBase == "" {
  r, err = s.fromManifest(ctx, source)
 } else {
		apiCtx := ctx
		if source == "auto" && s.APIBase == "" {
			var cancel context.CancelFunc
			apiCtx, cancel = context.WithTimeout(ctx, 9*time.Second)
			defer cancel()
		}
		err = s.fetchJSON(apiCtx, s.latestURL(), &r)
		r.Source = "official"
	}
	if err != nil && source == "auto" && s.APIBase == "" {
		r, err = s.fromManifest(ctx, "auto")
	}
	if err != nil {
		return ghRelease{}, ghAsset{}, ghAsset{}, fmt.Errorf("检查更新失败：%w", err)
	}
	if !regexp.MustCompile(`^v[0-9]+\.[0-9]+\.[0-9]+$`).MatchString(r.Tag) {
		return r, ghAsset{}, ghAsset{}, errors.New("Release 版本格式无效")
	}
	name := s.filename()
	var asset, checksum ghAsset
	for _, a := range r.Assets {
		if a.Name == name {
			asset = a
		}
		if a.Name == name+".sha256" {
			checksum = a
		}
	}
	if asset.Name != "" && s.APIBase == "" {
		asset.URL = s.releaseAssetURL(r.Tag, asset.Name)
	}
	if checksum.Name != "" && s.APIBase == "" {
		checksum.URL = s.releaseAssetURL(r.Tag, checksum.Name)
	}
	return r, asset, checksum, nil
}
func validHash(s string) bool { return regexp.MustCompile(`^[a-fA-F0-9]{64}$`).MatchString(s) }
func semver(v string) ([3]int, error) {
	var n [3]int
	p := strings.Split(strings.TrimPrefix(v, "v"), ".")
	if len(p) != 3 {
		return n, errors.New("版本号非法")
	}
	for i, x := range p {
		v, e := strconv.Atoi(x)
		if e != nil || v < 0 {
			return n, errors.New("版本号非法")
		}
		n[i] = v
	}
	return n, nil
}
func newer(a, b string) bool {
	x, e := semver(a)
	if e != nil {
		return false
	}
	y, e := semver(b)
	if e != nil {
		return false
	}
	for i := 0; i < 3; i++ {
		if x[i] != y[i] {
			return x[i] > y[i]
		}
	}
	return false
}
func (s *Service) Check(ctx context.Context, sources ...string) (Release, error) {
	source := "auto"
	if len(sources) > 0 && sources[0] != "" {
		source = sources[0]
	}
	r, asset, sum, err := s.latest(ctx, source)
	if err != nil {
		return Release{}, err
	}
	has := asset.Name != "" && (sum.Name != "" || validHash(asset.SHA))
	return Release{Version: r.Tag, Current: s.Version, UpdateAvailable: newer(r.Tag, s.Version) && has, Asset: asset.Name, URL: asset.URL, Notes: r.Body, HasSHA256: has, Size:asset.Size, Source: r.Source}, nil
}

// releasedVersions uses only GitHub Release metadata (no arbitrary URLs).
// The proxy sites mirror files but cannot reliably enumerate GitHub's history.
// Limit the result to the ten newest matching formal releases for this OS.
func (s *Service) releasedVersions(ctx context.Context) ([]ghRelease,error) {
 if !validateRepo(s.Repo){return nil,errors.New("无效 GitHub 仓库名")}
 endpoint:="https://api.github.com/repos/"+s.Repo+"/releases?per_page=40"
 if s.APIBase!="" {endpoint=strings.TrimRight(s.APIBase,"/")+"/releases?per_page=40"}
 var rows []struct {
  Tag string `json:"tag_name"`
  Body string `json:"body"`
  Draft bool `json:"draft"`
  Prerelease bool `json:"prerelease"`
  Assets []ghAsset `json:"assets"`
 }
 c,cancel:=context.WithTimeout(ctx,15*time.Second)
 defer cancel()
 bytes,err:=s.fetch(c,endpoint,4<<20)
 if err!=nil{return nil,fmt.Errorf("无法读取历史版本列表（需要访问 GitHub API）：%w",err)}
 if err=json.Unmarshal(bytes,&rows);err!=nil{return nil,err}
 out:=make([]ghRelease,0,10)
 seen:=make(map[string]bool)
 name:=s.filename()
 for _,r:=range rows{
  if r.Draft||r.Prerelease||!regexp.MustCompile(`^v[0-9]+.[0-9]+.[0-9]+$`).MatchString(r.Tag)||seen[r.Tag] {continue}
  found,hasSum:=false,false
  for _,a:=range r.Assets{
   if a.Name==name&&a.Size>0&&a.Size<=maxPackage {found=true}
   if a.Name==name+".sha256" {hasSum=true}
  }
  if !found||!hasSum {continue}
  seen[r.Tag]=true
  out=append(out,ghRelease{Tag:r.Tag,Body:r.Body,Assets:r.Assets,Source:"official"})
  if len(out)==10 {break}
 }
 return out,nil
}

func (s *Service) Versions(ctx context.Context)([]VersionChoice,error){
 rows,err:=s.releasedVersions(ctx)
 if err!=nil{return nil,err}
 out:=make([]VersionChoice,0,len(rows))
 for _,r:=range rows{
  for _,a:=range r.Assets{
   if a.Name==s.filename(){
    out=append(out,VersionChoice{Version:r.Tag,Size:a.Size,Current:"v"+s.Version==r.Tag,IsOlder:newer(s.Version,r.Tag),HasSHA256:true})
    break
   }
  }
 }
 return out,nil
}

func findAsset(r ghRelease,name string,service *Service)(ghAsset,ghAsset){
 var asset,checksum ghAsset
 for _,a:=range r.Assets{
  if a.Name==name{asset=a}
  if a.Name==name+".sha256"{checksum=a}
 }
 if service.APIBase==""{
  if asset.Name!="" {asset.URL=service.releaseAssetURL(r.Tag,asset.Name)}
  if checksum.Name!="" {checksum.URL=service.releaseAssetURL(r.Tag,checksum.Name)}
 }
 return asset,checksum
}

type progressWriter struct {
 callback func(DownloadProgress)
 snapshot DownloadProgress
 started time.Time
 last time.Time
}
func (w *progressWriter) Write(data []byte)(int,error){
 n:=len(data)
 w.snapshot.DownloadedBytes+=int64(n)
 now:=time.Now()
 if now.Sub(w.last)>=180*time.Millisecond||w.snapshot.DownloadedBytes>=w.snapshot.TotalBytes{
  if w.snapshot.TotalBytes>0 {w.snapshot.Percent=100*float64(w.snapshot.DownloadedBytes)/float64(w.snapshot.TotalBytes)}
  if elapsed:=now.Sub(w.started).Seconds();elapsed>0{w.snapshot.SpeedBPS=float64(w.snapshot.DownloadedBytes)/elapsed}
  if w.callback!=nil {w.callback(w.snapshot)}
  w.last=now
 }
 return n,nil
}
func (s *Service) downloadCandidate(ctx context.Context, u, source string, max int64) ([]byte, string, error) {
 candidates := sourceCandidates(u, source, s.APIBase != "")
 var last error
 for _, candidate := range candidates {
  downloadCtx, cancel := context.WithTimeout(ctx, 12*time.Second)
  data, e := s.fetch(downloadCtx, candidate, max)
  cancel()
  if e == nil { return data, candidate, nil }
  last=e
 }
 return nil, "", fmt.Errorf("更新校验文件所有所选线路均失败：%w", last)
}

func (s *Service) Download(ctx context.Context, sources ...string) (Downloaded, error) {
 return s.DownloadVersion(ctx,"",false,nil,sources...)
}

// DownloadVersion accepts only a server-listed recent formal release; a
// downgrade requires explicit opt-in and never bypasses SHA256 validation.
func (s *Service) DownloadVersion(ctx context.Context, target string, allowDowngrade bool, report func(DownloadProgress), sources ...string) (Downloaded, error) {
	source := "auto"
	if len(sources) > 0 && sources[0] != "" {
		source = sources[0]
	}
	if report!=nil{report(DownloadProgress{Phase:"checking",Version:target,Message:"正在读取版本信息与 SHA256 校验文件"})}
	var r ghRelease
	var asset,checksum ghAsset
	var e error
	if target==""{
		r,asset,checksum,e=s.latest(ctx,source)
	}else{
		if !regexp.MustCompile(`^v[0-9]+\.[0-9]+\.[0-9]+$`).MatchString(target){return Downloaded{},errors.New("指定版本号格式无效")}
		versions,err:=s.releasedVersions(ctx)
		if err!=nil{return Downloaded{},err}
		found:=false
		for _,row:=range versions{
			if row.Tag==target {r=row;found=true;break}
		}
		if !found{return Downloaded{},errors.New("指定版本不在最近十个可安装的正式发行版中")}
		asset,checksum=findAsset(r,s.filename(),s)
	}
	if e!=nil{return Downloaded{},e}
	if !newer(r.Tag,s.Version){
		if r.Tag=="v"+s.Version{return Downloaded{},errors.New("已经是当前版本，无须重复安装")}
		if !allowDowngrade{return Downloaded{},errors.New("降级需要用户明确确认")}
	}
	if asset.Name == "" || asset.Size < 1 || asset.Size > maxPackage || (checksum.Name == "" && !validHash(asset.SHA)) {
		return Downloaded{}, errors.New("Release 缺少当前平台的完整 ZIP 和 SHA-256")
	}
	hash := strings.ToLower(asset.SHA)
	if report!=nil{report(DownloadProgress{Phase:"verifying",Version:r.Tag,TotalBytes:asset.Size,Message:"正在取得校验信息"})}
	if checksum.Name != "" {
		b, _, err := s.downloadCandidate(ctx, checksum.URL, source, 64<<10)
		if err != nil {
			return Downloaded{}, err
		}
		fields := strings.Fields(string(b))
		if len(fields) < 1 || !validHash(fields[0]) {
			return Downloaded{}, errors.New("SHA-256 文件无效")
		}
		if len(fields) > 1 && strings.TrimPrefix(fields[1], "*") != asset.Name {
			return Downloaded{}, errors.New("SHA-256 文件名不符")
		}
		if hash != "" && !strings.EqualFold(hash, fields[0]) {
			return Downloaded{}, errors.New("更新清单与校验文件的 SHA-256 不一致")
		}
		hash = strings.ToLower(fields[0])
	}
	if !validHash(hash) {
		return Downloaded{}, errors.New("缺少有效 SHA-256")
	}
	// Restrict redirection origins to known GitHub-hosted assets, or to the
	// user-selected third-party proxy. No other metadata URLs are accepted.
	if s.APIBase == "" && asset.URL != s.releaseAssetURL(r.Tag, s.filename()) {
		return Downloaded{}, errors.New("非官方更新包地址")
	}
	dir := filepath.Join(s.Dir, "updates")
	if e = os.MkdirAll(dir, 0700); e != nil {
		return Downloaded{}, e
	}
	candidates := sourceCandidates(asset.URL, source, s.APIBase != "")
	var downloadErr error
	for _, u := range candidates {
		if report!=nil{report(DownloadProgress{Phase:"downloading",Version:r.Tag,Source:u,TotalBytes:asset.Size,Message:"正在下载发行包"})}
		// A dead community node must not block all later fallback candidates.
		limit := 30 * time.Second
		if source == "auto" && s.APIBase == "" && u == asset.URL { limit = 16 * time.Second }
		requestCtx, cancel := context.WithTimeout(ctx, limit)
		req, err := http.NewRequestWithContext(requestCtx, "GET", u, nil)
		if err != nil {
			cancel()
			return Downloaded{}, err
		}
		req.Header.Set("User-Agent", "bilipdj-go-updater/1.0")
		resp, err := s.client().Do(req)
        if err != nil {
         cancel()
         downloadErr = err
         continue
        }
        if resp.StatusCode != 200 {
         downloadErr = fmt.Errorf("下载 HTTP %d", resp.StatusCode)
         resp.Body.Close()
         cancel()
         continue
        }
		tmp, err := os.CreateTemp(dir, ".download-*")
		if err != nil {
         resp.Body.Close()
         cancel()
         return Downloaded{}, err
        }
		digest := sha256.New()
		pw:=&progressWriter{callback:report,snapshot:DownloadProgress{Phase:"downloading",Version:r.Tag,Source:u,TotalBytes:asset.Size,Message:"正在下载发行包"},started:time.Now()}
		n, err := io.Copy(io.MultiWriter(tmp, digest,pw), io.LimitReader(resp.Body, maxPackage+1))
		resp.Body.Close()
        cancel()
		closeErr := tmp.Close()
		if err == nil {
			err = closeErr
		}
		if err == nil && n != asset.Size {
			err = errors.New("下载大小与 Release 不一致")
		}
		if err == nil && n > maxPackage {
			err = errors.New("下载体积超出上限")
		}
		if report!=nil{report(DownloadProgress{Phase:"verifying",Version:r.Tag,DownloadedBytes:n,TotalBytes:asset.Size,Percent:100,Message:"下载完成，正在执行 SHA256 完整性验证"})}
		if err == nil && !strings.EqualFold(hex.EncodeToString(digest.Sum(nil)), hash) {
			err = errors.New("SHA256 不匹配")
		}
		if err != nil {
			os.Remove(tmp.Name())
			downloadErr = err
			continue
		}
		target := filepath.Join(dir, strings.TrimPrefix(r.Tag, "v")+"-"+asset.Name)
		if err = os.Rename(tmp.Name(), target); err != nil {
			os.Remove(tmp.Name())
			return Downloaded{}, err
		}
		if report!=nil{report(DownloadProgress{Phase:"ready",Version:r.Tag,DownloadedBytes:n,TotalBytes:asset.Size,Percent:100,Message:"下载及 SHA256 验证通过，可安装"})}
		return Downloaded{File: target, SHA256: hash, Version: r.Tag, Message: "更新包已验证并下载；可以点击安装并重启"}, nil
	}
	return Downloaded{}, fmt.Errorf("更新包所有线路均失败：%w", downloadErr)
}
