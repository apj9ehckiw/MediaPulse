package monitor

import (
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
	"time"

	"github.com/apj9ehckiw/mediapulse/backend/internal/config"
	"github.com/apj9ehckiw/mediapulse/backend/internal/history"
)

func newTestMonitor(t *testing.T, apiBase string) (*Monitor, string) {
	t.Helper()
	dir := t.TempDir()
	seed := config.Default()
	seed.APIBase = apiBase
	seed.Authors = []config.AuthorConfig{{UID: 168672751201, Note: "", Enabled: true}}
	store, err := config.Open(dir+"/config.json", seed)
	if err != nil {
		t.Fatal(err)
	}
	hist, err := history.Open(dir + "/downloads.json")
	if err != nil {
		t.Fatal(err)
	}
	m := New(store, Paths{OutDir: dir, StateFile: dir + "/state.json"}, hist)
	m.Run()
	t.Cleanup(func() {
		select {
		case <-m.stopCh:
		default:
			close(m.stopCh)
		}
	})
	return m, dir
}

// 复现线上故障：站点不可达（连接黑洞，不响应也不拒绝）时，
// /api/status 等端点必须仍然正常返回——监控锁不能被网络路径卡死。
// （v1.8.2 及之前：checkAuthor 的 m.client 数据竞争在特定时序下 panic，
// panic 展开期间持锁导致全部 API 永久挂起、页面骨架屏永远转。）
func TestSnapshotNotBlockedByBlackholeNetwork(t *testing.T) {
	blackhole := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		select {case <-r.Context().Done():} // 挂起直到客户端断开
	}))
	// 先断开所有挂起连接再关服务（Close 会等活跃 handler，select{} 会卡死测试）
	t.Cleanup(func() {
		blackhole.CloseClientConnections()
		blackhole.Close()
	})

	m, _ := newTestMonitor(t, blackhole.URL)

	time.Sleep(500 * time.Millisecond) // 让 checkDue 进入 ListTopics 网络调用

	done := make(chan struct{})
	go func() {
		_ = m.Snapshot()
		close(done)
	}()
	select {
	case <-done:
		// 通过：Snapshot 不受网络路径影响
	case <-time.After(5 * time.Second):
		t.Fatal("Snapshot 挂起 5s —— 监控锁被网络路径卡死（线上 v1.8.2 故障）")
	}
}

// 并发替换客户端（模拟网页端改配置）+ 并发读取（模拟下载/检查/快照）：
// 验证无死锁、能正常收敛退出。
func TestClientReplaceConcurrentNoRace(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte(`{}`))
	}))
	defer srv.Close()

	m, _ := newTestMonitor(t, srv.URL)

	var wg sync.WaitGroup
	stop := make(chan struct{})
	go func() {
		time.Sleep(1500 * time.Millisecond)
		close(stop)
	}()

	// 写方：不断原子替换客户端（模拟配置变更）
	wg.Add(1)
	go func() {
		defer wg.Done()
		for {
			select {
			case <-stop:
				return
			default:
				m.replaceClient(srv.URL)
				time.Sleep(2 * time.Millisecond)
			}
		}
	}()

	// 读方：并发调用所有使用 client 的路径
	for i := 0; i < 4; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for {
				select {
				case <-stop:
					return
				default:
					_ = m.client().Base
					_ = m.Snapshot()
					_ = m.dl()
					time.Sleep(2 * time.Millisecond)
				}
			}
		}()
	}
	wg.Wait()
}
