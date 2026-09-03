// Package site 实现目标站点 API 客户端（接口约定见 config.json 的 apiBase）。
// 解析逻辑对应浏览器版 source/index.html：
//   - 响应体为三重 base64：外层两次 b64 得到一重 b64 字符串，
//     最后一重解出 UTF-8 JSON（bareDecode）
//   - 帖子列表 /api/topic/node/topics?userId=&type=1（type=1 即“最新”排序）
//   - 帖子详情 /api/topic/<id>，video 附件 remoteUrl 为 *_i_preview.m3u8
//   - 由 preview 播放列表段名推导完整 m3u8（getRealVideoSrc）
package site

import (
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"regexp"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"
)

// TopicAuthor 帖子作者信息（列表响应自带）。
type TopicAuthor struct {
	ID       int64  `json:"id"`
	Nickname string `json:"nickname"`
}

// Topic 帖子列表条目。
type Topic struct {
	TopicID      int64           `json:"topicId"`
	AuthorUID    int64           `json:"-"` // 由调用方补充（列表按作者拉取）
	Title        string          `json:"title"`
	CreateTime   string          `json:"createTime"`
	HasVideo     bool            `json:"hasVideo"`
	HasPic       bool            `json:"hasPic"`
	ViewCount    int64           `json:"viewCount"`
	CommentCount int64           `json:"commentCount"`
	MoneyType    int             `json:"money_type"`
	Attachments  json.RawMessage `json:"attachments"`
	User         TopicAuthor     `json:"user"`
}

// Page 列表分页信息。
type Page struct {
	Page  int `json:"page"`
	Limit int `json:"limit"`
	Total int `json:"total"`
}

type listResp struct {
	Page    Page    `json:"page"`
	Results []Topic `json:"results"`
}

// Attachment 帖子附件。
type Attachment struct {
	ID        int64  `json:"id"`
	Category  string `json:"category"`
	RemoteURL string `json:"remoteUrl"`
}

// TopicDetail 帖子详情。
type TopicDetail struct {
	TopicID     int64        `json:"topicId"`
	Title       string       `json:"title"`
	CreateTime  string       `json:"createTime"`
	Attachments []Attachment `json:"attachments"`
}

type apiEnvelope struct {
	IsEncrypted bool            `json:"isEncrypted"`
	ErrorCode   int             `json:"errorCode"`
	Message     string          `json:"message"`
	Success     bool            `json:"success"`
	Data        json.RawMessage `json:"data"`
}

var errRateLimited = errors.New("429 rate limited")

// Client 站点 API 客户端。
type Client struct {
	Base  string
	HTTP  *http.Client
	Limit int // 列表每页条数（服务端上限 50）
}

// New 创建客户端。
func New(base string) *Client {
	return &Client{
		Base: strings.TrimRight(base, "/"),
		HTTP: &http.Client{Timeout: 60 * time.Second},
		Limit: 50,
	}
}

// bareDecode 三重 base64 解密（index.html bareDecode）。
func bareDecode(text string) (json.RawMessage, error) {
	once, err := base64.StdEncoding.DecodeString(text)
	if err != nil {
		return nil, err
	}
	twice, err := base64.StdEncoding.DecodeString(string(once))
	if err != nil {
		return nil, err
	}
	raw, err := base64.StdEncoding.DecodeString(string(twice))
	if err != nil {
		return nil, err
	}
	if !utf8Valid(raw) {
		return nil, errors.New("bareDecode: invalid utf-8")
	}
	return json.RawMessage(raw), nil
}

func utf8Valid(b []byte) bool { return utf8.Valid(b) }

func (c *Client) get(path string, query url.Values) (json.RawMessage, error) {
	var lastErr error
	for attempt := 1; attempt <= 3; attempt++ {
		u := c.Base + path
		if len(query) > 0 {
			u += "?" + query.Encode()
		}
		req, err := http.NewRequest(http.MethodGet, u, nil)
		if err != nil {
			return nil, err
		}
		req.Header.Set("User-Agent", "Mozilla/5.0 (Windows NT 10.0; Win64; x64) Chrome/126.0")
		req.Header.Set("Referer", c.Base+"/")

		resp, err := c.HTTP.Do(req)
		if err != nil {
			lastErr = err
		} else {
			switch {
			case resp.StatusCode == http.StatusTooManyRequests:
				resp.Body.Close()
				lastErr = errRateLimited
			case resp.StatusCode != http.StatusOK:
				body, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
				resp.Body.Close()
				lastErr = fmt.Errorf("%s: http %d: %s", path, resp.StatusCode, strings.TrimSpace(string(body)))
			default:
				raw, err := decodeEnvelope(resp)
				resp.Body.Close()
				if err == nil {
					return raw, nil
				}
				lastErr = err
			}
		}
		if attempt < 3 {
			time.Sleep(time.Duration(attempt*3) * time.Second)
		}
	}
	return nil, lastErr
}

func decodeEnvelope(resp *http.Response) (json.RawMessage, error) {
	body, err := io.ReadAll(io.LimitReader(resp.Body, 64<<20))
	if err != nil {
		return nil, err
	}
	var env apiEnvelope
	if err := json.Unmarshal(body, &env); err != nil {
		return nil, fmt.Errorf("bad json: %w", err)
	}
	if !env.Success {
		return nil, fmt.Errorf("api error %d: %s", env.ErrorCode, env.Message)
	}
	if env.IsEncrypted && len(env.Data) > 0 && env.Data[0] == '"' {
		var s string
		if err := json.Unmarshal(env.Data, &s); err != nil {
			return nil, err
		}
		return bareDecode(s)
	}
	return env.Data, nil
}

// ListTopics 拉取作者全部帖子并按 createTime 降序（最新在前）。
// type: 0=全部 1=最新 3=精华（对应站内路由 /homepage/last/:uid 等）。
func (c *Client) ListTopics(uid int64, listType int) ([]Topic, error) {
	var all []Topic
	for page := 1; ; page++ {
		q := url.Values{}
		q.Set("page", strconv.Itoa(page))
		q.Set("limit", strconv.Itoa(c.Limit))
		q.Set("userId", strconv.FormatInt(uid, 10))
		q.Set("type", strconv.Itoa(listType))

		raw, err := c.get("/api/topic/node/topics", q)
		if err != nil {
			return nil, err
		}
		var lr listResp
		if err := json.Unmarshal(raw, &lr); err != nil {
			return nil, err
		}
		if len(lr.Results) == 0 {
			break
		}
		all = append(all, lr.Results...)
		if len(all) >= lr.Page.Total || page > 200 {
			break
		}
		time.Sleep(500 * time.Millisecond)
	}
	sortTopicsByTime(all)
	return all, nil
}

func sortTopicsByTime(topics []Topic) {
	// createTime 形如 "2026-07-16 12:21:49"，字符串序即时间序
	for i := 1; i < len(topics); i++ {
		for j := i; j > 0 && topics[j].CreateTime > topics[j-1].CreateTime; j-- {
			topics[j], topics[j-1] = topics[j-1], topics[j]
		}
	}
}

// AuthorInfo 取作者昵称与帖子总数（列表首页第一条的 user.nickname）。
func (c *Client) AuthorInfo(uid int64) (nickname string, total int, err error) {
	q := url.Values{}
	q.Set("page", "1")
	q.Set("limit", "1")
	q.Set("userId", strconv.FormatInt(uid, 10))
	q.Set("type", "1")
	raw, err := c.get("/api/topic/node/topics", q)
	if err != nil {
		return "", 0, err
	}
	var lr listResp
	if err := json.Unmarshal(raw, &lr); err != nil {
		return "", 0, err
	}
	total = lr.Page.Total
	if len(lr.Results) > 0 {
		nickname = lr.Results[0].User.Nickname
	}
	return nickname, total, nil
}

// Detail 拉取帖子详情。
func (c *Client) Detail(topicID int64) (*TopicDetail, error) {
	raw, err := c.get("/api/topic/"+strconv.FormatInt(topicID, 10), nil)
	if err != nil {
		return nil, err
	}
	var d TopicDetail
	if err := json.Unmarshal(raw, &d); err != nil {
		return nil, err
	}
	return &d, nil
}

// VideoM3u8 从详情附件提取视频 preview m3u8 URL。
func (d *TopicDetail) VideoM3u8() string {
	for _, a := range d.Attachments {
		if a.Category == "video" || strings.Contains(a.RemoteURL, ".m3u8") {
			return a.RemoteURL
		}
	}
	return ""
}

var (
	segTSRe   = regexp.MustCompile(`^(\d+)\.ts$`)
	nameTSRe  = regexp.MustCompile(`^(.*?)(\d+)\.ts(\?.*)?$`)
	previewRe = regexp.MustCompile(`(?i)_preview\.m3u8(\?|$)`)
)

// FetchBytes 下载任意站点资源（带重试）。
func (c *Client) FetchBytes(rawurl string) ([]byte, error) {
	var last error
	for attempt := 1; attempt <= 3; attempt++ {
		req, err := http.NewRequest(http.MethodGet, rawurl, nil)
		if err != nil {
			return nil, err
		}
		req.Header.Set("User-Agent", "Mozilla/5.0 (Windows NT 10.0; Win64; x64) Chrome/126.0")
		resp, err := c.HTTP.Do(req)
		if err == nil && resp.StatusCode == http.StatusOK {
			defer resp.Body.Close()
			return io.ReadAll(resp.Body)
		}
		if resp != nil {
			resp.Body.Close()
		}
		last = err
		if attempt < 3 {
			time.Sleep(time.Duration(attempt*2) * time.Second)
		}
	}
	return nil, last
}

// ResolveFullM3u8 由 preview 播放列表段名推导完整 m3u8（index.html getRealVideoSrc）。
// 例：.../515993_i_preview.m3u8 的段名 13523542VapFmBSe_i0.ts
//     → .../13523542VapFmBSe_i.m3u8（preview 仅含前若干段，完整版为全部段）。
func (c *Client) ResolveFullM3u8(previewURL string) (string, error) {
	if !previewRe.MatchString(previewURL) {
		return previewURL, nil
	}
	body, err := c.FetchBytes(previewURL)
	if err != nil {
		return "", err
	}
	base := previewURL[:strings.LastIndex(previewURL, "/")+1]
	for _, line := range strings.Split(string(body), "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		if m := nameTSRe.FindStringSubmatch(line); m != nil && m[1] != "" {
			return base + m[1] + ".m3u8", nil
		}
	}
	// 兜底：直接替换文件名后缀
	r := strings.NewReplacer("_i_preview.m3u8", ".m3u8", "_preview.m3u8", ".m3u8")
	return r.Replace(previewURL), nil
}

// Playlist 播放列表内容。
type Playlist struct {
	Segments []string // 相对名
	KeyURL   string   // 绝对 URL
	IVHex    string
}

// ParsePlaylist 拉取并解析 m3u8。
func (c *Client) ParsePlaylist(m3u8URL string) (*Playlist, error) {
	body, err := c.FetchBytes(m3u8URL)
	if err != nil {
		return nil, err
	}
	base := m3u8URL[:strings.LastIndex(m3u8URL, "/")+1]
	pl := &Playlist{}
	for _, line := range strings.Split(string(body), "\n") {
		line = strings.TrimSpace(line)
		switch {
		case strings.HasPrefix(line, "#EXT-X-KEY"):
			if m := regexp.MustCompile(`URI="([^"]+)"`).FindStringSubmatch(line); m != nil {
				if u, err := url.Parse(m[1]); err == nil && u.IsAbs() {
					pl.KeyURL = m[1]
				} else {
					pl.KeyURL = base + m[1]
				}
			}
			if m := regexp.MustCompile(`IV=0x([0-9a-fA-F]+)`).FindStringSubmatch(line); m != nil {
				pl.IVHex = strings.ToLower(m[1])
			}
		case line != "" && !strings.HasPrefix(line, "#"):
			if segTSRe.MatchString(line) || nameTSRe.MatchString(line) {
				pl.Segments = append(pl.Segments, line)
			}
		}
	}
	if len(pl.Segments) == 0 {
		return nil, errors.New("playlist has no segments")
	}
	return pl, nil
}
