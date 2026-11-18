package model

import (
	"context"
	"sync"
)

type inferenceRequest struct {
	isStream bool
	prompt   string
	opts     GenerationOptions
	onToken  func(string) bool
	ctx      context.Context
	respChan chan inferenceResponse
}

type inferenceResponse struct {
	text string
	err  error
}

// QueuedEngine wraps an InferenceEngine with a FIFO request queue and background worker
// to serialize inference requests and handle context timeouts while waiting in queue.
type QueuedEngine struct {
	engine     InferenceEngine
	reqChan    chan inferenceRequest
	priorityCh chan inferenceRequest
	shutdownCh chan struct{}
	wg         sync.WaitGroup
	closeOnce  sync.Once
}

func NewQueuedEngine(engine InferenceEngine, bufferSize int) *QueuedEngine {
	if bufferSize <= 0 {
		bufferSize = 64
	}
	qe := &QueuedEngine{
		engine:     engine,
		reqChan:    make(chan inferenceRequest, bufferSize),
		priorityCh: make(chan inferenceRequest, bufferSize),
		shutdownCh: make(chan struct{}),
	}
	qe.wg.Add(1)
	go qe.workerLoop()
	return qe
}

func (qe *QueuedEngine) Name() string {
	return qe.engine.Name()
}

func (qe *QueuedEngine) ModelLabel() string {
	return EngineLabel(qe.engine)
}

func (qe *QueuedEngine) workerLoop() {
	defer qe.wg.Done()
	for {
		// Prefer action decisions over background reflections when both are queued.
		select {
		case <-qe.shutdownCh:
			return
		case req := <-qe.priorityCh:
			qe.processRequest(req)
			continue
		default:
		}
		select {
		case <-qe.shutdownCh:
			return
		case req := <-qe.priorityCh:
			qe.processRequest(req)
		case req := <-qe.reqChan:
			qe.processRequest(req)
		}
	}
}

func (qe *QueuedEngine) processRequest(req inferenceRequest) {
	if err := req.ctx.Err(); err != nil {
		req.respChan <- inferenceResponse{err: err}
		close(req.respChan)
		return
	}
	var resp string
	var err error
	if req.isStream {
		err = qe.engine.Stream(req.ctx, req.prompt, req.opts, req.onToken)
	} else {
		resp, err = qe.engine.Generate(req.ctx, req.prompt, req.opts)
	}
	select {
	case req.respChan <- inferenceResponse{text: resp, err: err}:
	default:
	}
	close(req.respChan)
}

func (qe *QueuedEngine) Generate(ctx context.Context, prompt string, opts GenerationOptions) (string, error) {
	return qe.generate(ctx, prompt, opts, false)
}

// GeneratePriority queues action decisions ahead of background synthesis work.
func (qe *QueuedEngine) GeneratePriority(ctx context.Context, prompt string, opts GenerationOptions) (string, error) {
	return qe.generate(ctx, prompt, opts, true)
}

func (qe *QueuedEngine) generate(ctx context.Context, prompt string, opts GenerationOptions, priority bool) (string, error) {
	respChan := make(chan inferenceResponse, 1)
	queue := qe.reqChan
	if priority {
		queue = qe.priorityCh
	}

	select {
	case <-ctx.Done():
		return "", ctx.Err()
	case queue <- inferenceRequest{
		isStream: false,
		prompt:   prompt,
		opts:     opts,
		ctx:      ctx,
		respChan: respChan,
	}:
	}

	select {
	case <-ctx.Done():
		return "", ctx.Err()
	case res := <-respChan:
		return res.text, res.err
	}
}

func (qe *QueuedEngine) Stream(ctx context.Context, prompt string, opts GenerationOptions, onToken func(string) bool) error {
	respChan := make(chan inferenceResponse, 1)

	select {
	case <-ctx.Done():
		return ctx.Err()
	case qe.reqChan <- inferenceRequest{
		isStream: true,
		prompt:   prompt,
		opts:     opts,
		onToken:  onToken,
		ctx:      ctx,
		respChan: respChan,
	}:
	}

	select {
	case <-ctx.Done():
		return ctx.Err()
	case res := <-respChan:
		return res.err
	}
}

func (qe *QueuedEngine) Close() error {
	qe.closeOnce.Do(func() {
		close(qe.shutdownCh)
	})
	qe.wg.Wait()
	return qe.engine.Close()
}

var _ InferenceEngine = (*QueuedEngine)(nil)
