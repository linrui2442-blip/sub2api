package service

import (
	"context"
	"fmt"
	"os"
	"sync"
	"sync/atomic"
	"time"
)

const (
	auditLogQueueCapacity = 4096
	auditLogBatchSize     = 100
	auditLogFlushInterval = time.Second

	auditRetentionCheckInterval = 24 * time.Hour
	auditRetentionBatchSize     = 5000
	auditRetentionPeriod        = 7 * 24 * time.Hour
)

type auditLogClearResult struct {
	deleted int64
	err     error
}

type auditLogClearRequest struct {
	ctx    context.Context
	result chan auditLogClearResult
}

// AuditLogService 管理面操作审计日志服务。
// 写入端为非阻塞异步批量落库（不拖慢管理请求）；
// 读取端提供分页查询；Personal Edition 固定仅保留最近 7 天。
type AuditLogService struct {
	repo AuditLogRepository

	queue         chan *AuditLog
	clearRequests chan auditLogClearRequest

	ctx    context.Context
	cancel context.CancelFunc
	wg     sync.WaitGroup

	droppedCount uint64
	writeFailed  uint64
	writtenCount uint64
	started      atomic.Bool

	now               func() time.Time
	retentionInterval time.Duration
}

func NewAuditLogService(repo AuditLogRepository) *AuditLogService {
	ctx, cancel := context.WithCancel(context.Background())
	return &AuditLogService{
		repo:              repo,
		queue:             make(chan *AuditLog, auditLogQueueCapacity),
		clearRequests:     make(chan auditLogClearRequest),
		ctx:               ctx,
		cancel:            cancel,
		now:               func() time.Time { return time.Now().UTC() },
		retentionInterval: auditRetentionCheckInterval,
	}
}

// Start 启动异步写入与保留期清理协程。
func (s *AuditLogService) Start() {
	if s == nil || s.repo == nil || !s.started.CompareAndSwap(false, true) {
		return
	}
	s.wg.Add(2)
	go s.runWriter()
	go s.runRetentionLoop()
}

// Stop 停止服务并尽量落盘队列中剩余记录。
func (s *AuditLogService) Stop() {
	if s == nil {
		return
	}
	s.cancel()
	s.wg.Wait()
}

// Record 非阻塞入队一条审计记录；队列打满时丢弃并计数（管理面流量下几乎不可能发生）。
func (s *AuditLogService) Record(entry *AuditLog) {
	if s == nil || entry == nil {
		return
	}
	if entry.CreatedAt.IsZero() {
		entry.CreatedAt = time.Now().UTC()
	}
	select {
	case <-s.ctx.Done():
		return
	default:
	}
	select {
	case s.queue <- entry:
	default:
		atomic.AddUint64(&s.droppedCount, 1)
	}
}

// List 分页查询审计日志。
func (s *AuditLogService) List(ctx context.Context, filter *AuditLogFilter) (*AuditLogList, error) {
	return s.repo.List(ctx, filter)
}

// GetByID 查询单条详情。
func (s *AuditLogService) GetByID(ctx context.Context, id int64) (*AuditLog, error) {
	return s.repo.GetByID(ctx, id)
}

// ClearAll 清空全部审计日志。该内部维护操作本身不写审计记录，确保
// 返回成功后列表确实为空，也避免清理行为递归产生新的审计日志。
func (s *AuditLogService) ClearAll(ctx context.Context) (int64, error) {
	if s.started.Load() {
		result := make(chan auditLogClearResult, 1)
		request := auditLogClearRequest{ctx: ctx, result: result}
		select {
		case s.clearRequests <- request:
		case <-ctx.Done():
			return 0, ctx.Err()
		case <-s.ctx.Done():
			return 0, context.Canceled
		}
		select {
		case outcome := <-result:
			return outcome.deleted, outcome.err
		case <-ctx.Done():
			return 0, ctx.Err()
		case <-s.ctx.Done():
			return 0, context.Canceled
		}
	}
	return s.clearAllDirect(ctx)
}

func (s *AuditLogService) clearAllDirect(ctx context.Context) (int64, error) {
	deleted, err := s.repo.Count(ctx)
	if err != nil {
		return 0, fmt.Errorf("count audit logs: %w", err)
	}
	if err := s.repo.TruncateAll(ctx); err != nil {
		return 0, fmt.Errorf("truncate audit logs: %w", err)
	}
	return deleted, nil
}

func (s *AuditLogService) runWriter() {
	defer s.wg.Done()

	ticker := time.NewTicker(auditLogFlushInterval)
	defer ticker.Stop()

	batch := make([]*AuditLog, 0, auditLogBatchSize)
	flush := func() {
		if len(batch) == 0 {
			return
		}
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		inserted, err := s.repo.BatchInsert(ctx, batch)
		cancel()
		if err != nil {
			atomic.AddUint64(&s.writeFailed, uint64(len(batch)))
			_, _ = fmt.Fprintf(os.Stderr, "time=%s level=WARN msg=\"audit log flush failed\" err=%v batch=%d\n",
				time.Now().Format(time.RFC3339Nano), err, len(batch))
		} else {
			atomic.AddUint64(&s.writtenCount, uint64(inserted))
		}
		batch = batch[:0]
	}

	for {
		select {
		case <-s.ctx.Done():
			// 停机前排空队列。
			for {
				select {
				case item := <-s.queue:
					if item == nil {
						continue
					}
					batch = append(batch, item)
					if len(batch) >= auditLogBatchSize {
						flush()
					}
				default:
					flush()
					return
				}
			}
		case item := <-s.queue:
			if item == nil {
				continue
			}
			batch = append(batch, item)
			if len(batch) >= auditLogBatchSize {
				flush()
			}
		case <-ticker.C:
			flush()
		case request := <-s.clearRequests:
			// 丢弃尚未落盘的旧审计事件，再清空持久层。清空接口自身由
			// middleware.SkipAudit 排除，因此成功返回后不会马上重现一条日志。
			batch = batch[:0]
			for draining := true; draining; {
				select {
				case <-s.queue:
				default:
					draining = false
				}
			}
			deleted, err := s.clearAllDirect(request.ctx)
			request.result <- auditLogClearResult{deleted: deleted, err: err}
		}
	}
}

// runRetentionLoop 按保留期定期删除过期审计日志。
// 删除操作幂等，多实例并发执行无害，因此无需选主。
func (s *AuditLogService) runRetentionLoop() {
	defer s.wg.Done()

	// 启动后立即做一次 best-effort 清理；失败只记录告警，不影响主程序。
	s.runRetentionOnce()

	ticker := time.NewTicker(s.retentionInterval)
	defer ticker.Stop()
	for {
		select {
		case <-s.ctx.Done():
			return
		case <-ticker.C:
			s.runRetentionOnce()
		}
	}
}

func (s *AuditLogService) runRetentionOnce() {
	ctx, cancel := context.WithTimeout(s.ctx, 10*time.Minute)
	defer cancel()

	cutoff := s.now().UTC().Add(-auditRetentionPeriod)
	for {
		deleted, err := s.repo.DeleteBefore(ctx, cutoff, auditRetentionBatchSize)
		if err != nil {
			_, _ = fmt.Fprintf(os.Stderr, "time=%s level=WARN msg=\"audit log retention cleanup failed\" err=%v\n",
				time.Now().Format(time.RFC3339Nano), err)
			return
		}
		if deleted == 0 {
			return
		}
	}
}
