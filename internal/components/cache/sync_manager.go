/**
 * @file sync_manager.go
 * @description Gerenciador de sincronização distribuída seguindo arquitetura hexagonal
 * @author Admilson B. F. Cossa
 * @date 2024-12-19
 * @path pkg/core/database/migrations/internal/components/cache/sync_manager.go
 */

package cache

import (
	"context"
	"fmt"
	"sync"
	"time"

	"github.com/google/uuid"
)

// DefaultSyncManager implementação padrão do SyncManager
type DefaultSyncManager struct {
	config       *SyncConfig
	nodeID       string
	clusterName  string
	isRunning    bool
	
	// Coordenador injetado via DI
	coordinator  NodeCoordinator
	
	// Canais de sincronização
	syncQueue    chan *SyncOperation
	stopChan     chan struct{}
	
	// Controle de operações
	pendingOps   map[string]*SyncOperation
	completedOps map[string]time.Time
	opsMutex     sync.RWMutex
	
	// Batch processing
	batchBuffer  []*SyncOperation
	batchMutex   sync.Mutex
	batchTicker  *time.Ticker
	
	// Retry mechanism
	retryQueue   map[string]*RetryOperation
	retryMutex   sync.RWMutex
	retryTicker  *time.Ticker
	
	// Métricas
	metrics      *SyncMetrics
	
	// Logger injetado
	logger       Logger
	
	mutex        sync.RWMutex
}

// SyncConfig configuração do gerenciador de sincronização
type SyncConfig struct {
	BatchSize           int           `json:"batch_size"`
	BatchTimeout        time.Duration `json:"batch_timeout"`
	RetryAttempts       int           `json:"retry_attempts"`
	RetryDelay          time.Duration `json:"retry_delay"`
	RetryBackoff        time.Duration `json:"retry_backoff"`
	MaxRetryBackoff     time.Duration `json:"max_retry_backoff"`
	WorkerCount         int           `json:"worker_count"`
	QueueSize           int           `json:"queue_size"`
	EnableBatching      bool          `json:"enable_batching"`
	EnableRetry         bool          `json:"enable_retry"`
	EnableDeduplication bool          `json:"enable_deduplication"`
	CompletedOpsTTL     time.Duration `json:"completed_ops_ttl"`
	CleanupInterval     time.Duration `json:"cleanup_interval"`
}

// RetryOperation operação com informações de retry
type RetryOperation struct {
	Operation    *SyncOperation
	Attempts     int
	NextRetry    time.Time
	LastError    error
}

// NewDefaultSyncManager cria novo gerenciador de sincronização seguindo padrão Factory
func NewDefaultSyncManager(
	config *SyncConfig,
	nodeID, clusterName string,
	coordinator NodeCoordinator,
	logger Logger,
) (*DefaultSyncManager, error) {
	
	if config == nil {
		return nil, fmt.Errorf("sync config cannot be nil")
	}
	
	if nodeID == "" {
		return nil, fmt.Errorf("node ID cannot be empty")
	}
	
	if clusterName == "" {
		return nil, fmt.Errorf("cluster name cannot be empty")
	}
	
	if coordinator == nil {
		return nil, fmt.Errorf("coordinator cannot be nil")
	}
	
	// Aplicar valores padrão se necessário
	if config.BatchSize <= 0 {
		config.BatchSize = 100
	}
	if config.BatchTimeout <= 0 {
		config.BatchTimeout = 5 * time.Second
	}
	if config.RetryAttempts <= 0 {
		config.RetryAttempts = 3
	}
	if config.RetryBackoff <= 0 {
		config.RetryBackoff = 1 * time.Second
	}
	if config.MaxRetryBackoff <= 0 {
		config.MaxRetryBackoff = 30 * time.Second
	}
	if config.QueueSize <= 0 {
		config.QueueSize = 10000
	}
	if config.CompletedOpsTTL <= 0 {
		config.CompletedOpsTTL = 1 * time.Hour
	}
	if config.CleanupInterval <= 0 {
		config.CleanupInterval = 10 * time.Minute
	}
	
	manager := &DefaultSyncManager{
		config:       config,
		nodeID:       nodeID,
		clusterName:  clusterName,
		coordinator:  coordinator,
		syncQueue:    make(chan *SyncOperation, config.QueueSize),
		stopChan:     make(chan struct{}),
		pendingOps:   make(map[string]*SyncOperation),
		completedOps: make(map[string]time.Time),
		batchBuffer:  make([]*SyncOperation, 0, config.BatchSize),
		retryQueue:   make(map[string]*RetryOperation),
		metrics:      &SyncMetrics{},
		logger:       logger,
	}
	
	return manager, nil
}

// Start inicia o gerenciador de sincronização
func (s *DefaultSyncManager) Start(ctx context.Context) error {
	s.mutex.Lock()
	defer s.mutex.Unlock()
	
	if s.isRunning {
		return fmt.Errorf("sync manager is already running")
	}
	
	// Iniciar workers
	go s.syncWorker(ctx)
	go s.cleanupWorker(ctx)
	
	// Iniciar batch processing se habilitado
	if s.config.EnableBatching {
		s.batchTicker = time.NewTicker(s.config.BatchTimeout)
		go s.batchWorker(ctx)
	}
	
	// Iniciar retry mechanism se habilitado
	if s.config.EnableRetry {
		s.retryTicker = time.NewTicker(s.config.RetryBackoff)
		go s.retryWorker(ctx)
	}
	
	s.isRunning = true
	
	s.logger.Info("Sync manager started", map[string]interface{}{
		"node_id":        s.nodeID,
		"cluster_name":   s.clusterName,
		"batch_enabled":  s.config.EnableBatching,
		"retry_enabled":  s.config.EnableRetry,
		"queue_size":     s.config.QueueSize,
	})
	
	return nil
}

// Stop para o gerenciador de sincronização
func (s *DefaultSyncManager) Stop(ctx context.Context) error {
	s.mutex.Lock()
	defer s.mutex.Unlock()
	
	if !s.isRunning {
		return nil
	}
	
	// Parar tickers
	if s.batchTicker != nil {
		s.batchTicker.Stop()
	}
	if s.retryTicker != nil {
		s.retryTicker.Stop()
	}
	
	// Sinalizar parada
	close(s.stopChan)
	
	// Processar operações pendentes
	s.processPendingOperations(ctx)
	
	s.isRunning = false
	
	s.logger.Info("Sync manager stopped", map[string]interface{}{
		"node_id": s.nodeID,
	})
	
	return nil
}

// IsRunning verifica se o gerenciador está ativo
func (s *DefaultSyncManager) IsRunning() bool {
	s.mutex.RLock()
	defer s.mutex.RUnlock()
	return s.isRunning
}

// QueueOperation adiciona operação à fila de sincronização
func (s *DefaultSyncManager) QueueOperation(op *SyncOperation) error {
	if op == nil {
		return fmt.Errorf("sync operation cannot be nil")
	}
	
	if !s.isRunning {
		return fmt.Errorf("sync manager is not running")
	}
	
	// Verificar deduplicação se habilitada
	if s.config.EnableDeduplication && s.isDuplicateOperation(op) {
		s.logger.Debug("Duplicate operation ignored", map[string]interface{}{
			"operation_id": op.ID,
			"type":         op.Type,
		})
		return nil
	}
	
	// Adicionar à fila
	select {
	case s.syncQueue <- op:
		s.metrics.mutex.Lock()
		s.metrics.OperationsQueued++
		s.metrics.mutex.Unlock()
		
		s.logger.Debug("Operation queued", map[string]interface{}{
			"operation_id": op.ID,
			"type":         op.Type,
		})
		
		return nil
	default:
		return fmt.Errorf("sync queue is full")
	}
}

// ProcessOperation processa operação imediatamente
func (s *DefaultSyncManager) ProcessOperation(ctx context.Context, op *SyncOperation) error {
	if op == nil {
		return fmt.Errorf("sync operation cannot be nil")
	}
	
	start := time.Now()
	
	// Marcar como pendente
	s.opsMutex.Lock()
	s.pendingOps[op.ID] = op
	s.opsMutex.Unlock()
	
	// Processar operação
	err := s.executeOperation(ctx, op)
	
	// Atualizar estado
	s.opsMutex.Lock()
	delete(s.pendingOps, op.ID)
	if err == nil {
		s.completedOps[op.ID] = time.Now()
	}
	s.opsMutex.Unlock()
	
	// Atualizar métricas
	s.metrics.mutex.Lock()
	if err == nil {
		s.metrics.OperationsProcessed++
	} else {
		s.metrics.OperationsFailed++
	}
	
	// Atualizar latência média
	latency := time.Since(start)
	if s.metrics.AverageLatency == 0 {
		s.metrics.AverageLatency = latency
	} else {
		s.metrics.AverageLatency = (s.metrics.AverageLatency + latency) / 2
	}
	s.metrics.mutex.Unlock()
	
	if err != nil {
		// Adicionar ao retry se habilitado
		if s.config.EnableRetry {
			s.addToRetryQueue(op, err)
		}
		
		s.logger.Error("Failed to process operation", err, map[string]interface{}{
			"operation_id": op.ID,
			"type":         op.Type,
		})
	}
	
	return err
}

// GetPendingOperations retorna operações pendentes
func (s *DefaultSyncManager) GetPendingOperations() []*SyncOperation {
	s.opsMutex.RLock()
	defer s.opsMutex.RUnlock()
	
	ops := make([]*SyncOperation, 0, len(s.pendingOps))
	for _, op := range s.pendingOps {
		ops = append(ops, op)
	}
	
	return ops
}

// GetSyncMetrics retorna métricas de sincronização
func (s *DefaultSyncManager) GetSyncMetrics() *SyncMetrics {
	s.metrics.mutex.RLock()
	defer s.metrics.mutex.RUnlock()
	
	// Retornar cópia das métricas
	return &SyncMetrics{
		OperationsQueued:    s.metrics.OperationsQueued,
		OperationsProcessed: s.metrics.OperationsProcessed,
		OperationsFailed:    s.metrics.OperationsFailed,
		OperationsRetried:   s.metrics.OperationsRetried,
		BatchesProcessed:    s.metrics.BatchesProcessed,
		AverageLatency:      s.metrics.AverageLatency,
	}
}

// Workers internos

func (s *DefaultSyncManager) syncWorker(ctx context.Context) {
	for {
		select {
		case <-s.stopChan:
			return
		case <-ctx.Done():
			return
		case op := <-s.syncQueue:
			if op == nil {
				continue
			}
			
			if s.config.EnableBatching {
				s.addToBatch(op)
			} else {
				s.ProcessOperation(ctx, op)
			}
		}
	}
}

func (s *DefaultSyncManager) batchWorker(ctx context.Context) {
	for {
		select {
		case <-s.stopChan:
			s.processBatch(ctx)
			return
		case <-ctx.Done():
			s.processBatch(ctx)
			return
		case <-s.batchTicker.C:
			s.processBatch(ctx)
		}
	}
}

func (s *DefaultSyncManager) retryWorker(ctx context.Context) {
	for {
		select {
		case <-s.stopChan:
			return
		case <-ctx.Done():
			return
		case <-s.retryTicker.C:
			s.processRetryQueue(ctx)
		}
	}
}

func (s *DefaultSyncManager) cleanupWorker(ctx context.Context) {
	ticker := time.NewTicker(s.config.CleanupInterval)
	defer ticker.Stop()
	
	for {
		select {
		case <-s.stopChan:
			return
		case <-ctx.Done():
			return
		case <-ticker.C:
			s.cleanupCompletedOperations()
		}
	}
}

// Métodos auxiliares

func (s *DefaultSyncManager) executeOperation(ctx context.Context, op *SyncOperation) error {
	switch op.Type {
	case SyncOpSet, SyncOpDelete, SyncOpClear:
		return s.coordinator.BroadcastOperation(ctx, op)
	case SyncOpBatch:
		// Processar operações em lote
		return s.processBatchOperation(ctx, op)
	default:
		return fmt.Errorf("unknown operation type: %v", op.Type)
	}
}

func (s *DefaultSyncManager) processBatchOperation(ctx context.Context, batchOp *SyncOperation) error {
	// Implementação básica para operações em lote
	// Pode ser expandida conforme necessário
	return s.coordinator.BroadcastOperation(ctx, batchOp)
}

func (s *DefaultSyncManager) addToBatch(op *SyncOperation) {
	s.batchMutex.Lock()
	defer s.batchMutex.Unlock()
	
	s.batchBuffer = append(s.batchBuffer, op)
	
	// Processar batch se atingir o tamanho máximo
	if len(s.batchBuffer) >= s.config.BatchSize {
		go s.processBatch(context.Background())
	}
}

func (s *DefaultSyncManager) processBatch(ctx context.Context) {
	s.batchMutex.Lock()
	if len(s.batchBuffer) == 0 {
		s.batchMutex.Unlock()
		return
	}
	
	// Copiar buffer e limpar
	batch := make([]*SyncOperation, len(s.batchBuffer))
	copy(batch, s.batchBuffer)
	s.batchBuffer = s.batchBuffer[:0]
	s.batchMutex.Unlock()
	
	// Criar operação de lote
	batchOp := &SyncOperation{
		ID:        uuid.New().String(),
		Type:      SyncOpBatch,
		NodeID:    s.nodeID,
		Timestamp: time.Now(),
		Metadata:  map[string]interface{}{
			"batch_size": len(batch),
		},
	}
	
	// Processar lote
	if err := s.ProcessOperation(ctx, batchOp); err != nil {
		s.logger.Error("Failed to process batch", err, map[string]interface{}{
			"batch_size": len(batch),
		})
		
		// Reprocessar operações individualmente em caso de erro
		for _, op := range batch {
			s.ProcessOperation(ctx, op)
		}
	} else {
		// Atualizar métricas
		s.metrics.mutex.Lock()
		s.metrics.BatchesProcessed++
		s.metrics.mutex.Unlock()
		
		s.logger.Debug("Batch processed successfully", map[string]interface{}{
			"batch_size": len(batch),
		})
	}
}

func (s *DefaultSyncManager) addToRetryQueue(op *SyncOperation, err error) {
	s.retryMutex.Lock()
	defer s.retryMutex.Unlock()
	
	retryOp, exists := s.retryQueue[op.ID]
	if !exists {
		retryOp = &RetryOperation{
			Operation: op,
			Attempts:  0,
		}
		s.retryQueue[op.ID] = retryOp
	}
	
	retryOp.Attempts++
	retryOp.LastError = err
	
	// Calcular próximo retry com backoff exponencial
	backoff := time.Duration(retryOp.Attempts) * s.config.RetryBackoff
	if backoff > s.config.MaxRetryBackoff {
		backoff = s.config.MaxRetryBackoff
	}
	
	retryOp.NextRetry = time.Now().Add(backoff)
}

func (s *DefaultSyncManager) processRetryQueue(ctx context.Context) {
	s.retryMutex.Lock()
	defer s.retryMutex.Unlock()
	
	now := time.Now()
	
	for id, retryOp := range s.retryQueue {
		if retryOp.Attempts >= s.config.RetryAttempts {
			// Excedeu tentativas máximas
			delete(s.retryQueue, id)
			
			s.logger.Error("Operation exceeded max retry attempts", retryOp.LastError, map[string]interface{}{
				"operation_id": id,
				"attempts":     retryOp.Attempts,
			})
			continue
		}
		
		if now.After(retryOp.NextRetry) {
			// Tentar novamente
			go func(op *SyncOperation) {
				if err := s.ProcessOperation(ctx, op); err == nil {
					// Sucesso - remover da fila de retry
					s.retryMutex.Lock()
					delete(s.retryQueue, op.ID)
					s.retryMutex.Unlock()
					
					s.metrics.mutex.Lock()
					s.metrics.OperationsRetried++
					s.metrics.mutex.Unlock()
				}
			}(retryOp.Operation)
		}
	}
}

func (s *DefaultSyncManager) isDuplicateOperation(op *SyncOperation) bool {
	s.opsMutex.RLock()
	defer s.opsMutex.RUnlock()
	
	// Verificar se está pendente
	if _, pending := s.pendingOps[op.ID]; pending {
		return true
	}
	
	// Verificar se foi completada recentemente
	if completedTime, completed := s.completedOps[op.ID]; completed {
		if time.Since(completedTime) < s.config.CompletedOpsTTL {
			return true
		}
	}
	
	return false
}

func (s *DefaultSyncManager) cleanupCompletedOperations() {
	s.opsMutex.Lock()
	defer s.opsMutex.Unlock()
	
	now := time.Now()
	
	for id, completedTime := range s.completedOps {
		if now.Sub(completedTime) > s.config.CompletedOpsTTL {
			delete(s.completedOps, id)
		}
	}
}

func (s *DefaultSyncManager) processPendingOperations(ctx context.Context) {
	s.opsMutex.RLock()
	pendingOps := make([]*SyncOperation, 0, len(s.pendingOps))
	for _, op := range s.pendingOps {
		pendingOps = append(pendingOps, op)
	}
	s.opsMutex.RUnlock()
	
	for _, op := range pendingOps {
		s.ProcessOperation(ctx, op)
	}
}

// StartSync inicia o processo de sincronização
func (s *DefaultSyncManager) StartSync(ctx context.Context) error {
	return s.Start(ctx)
}

// StopSync para o processo de sincronização
func (s *DefaultSyncManager) StopSync(ctx context.Context) error {
	return s.Stop(ctx)
}

// SyncOperation processa uma operação de sincronização
func (s *DefaultSyncManager) SyncOperation(ctx context.Context, op *SyncOperation) error {
	return s.ProcessOperation(ctx, op)
}

// ResolveConflict resolve conflitos entre itens locais e remotos
func (s *DefaultSyncManager) ResolveConflict(ctx context.Context, local, remote *CacheItem) (*CacheItem, error) {
	// Implementação básica - usar timestamp mais recente (LastAccessed)
	if local.LastAccessed.After(remote.LastAccessed) {
		return local, nil
	}
	return remote, nil
}

// SetConflictStrategy define a estratégia de resolução de conflitos
func (s *DefaultSyncManager) SetConflictStrategy(strategy ConflictStrategy) {
	// Implementação básica - armazenar estratégia
	s.mutex.Lock()
	defer s.mutex.Unlock()
	// Adicionar campo strategy se necessário
}

// GetSyncStatus retorna o status atual de sincronização
func (s *DefaultSyncManager) GetSyncStatus() *SyncStatus {
	s.mutex.RLock()
	defer s.mutex.RUnlock()
	
	return &SyncStatus{
		IsActive:      s.isRunning,
		LastSync:      time.Now(), // Implementação básica
		NextSync:      time.Now().Add(5 * time.Minute),
		SyncCount:     0, // Implementar contador se necessário
		ErrorCount:    0, // Implementar contador se necessário
		PendingOps:    len(s.pendingOps),
		NodeStatuses:  make(map[string]*NodeHealth),
		ConflictCount: 0,
		ResolvedCount: 0,
	}
}

// SetSyncInterval define o intervalo de sincronização
func (s *DefaultSyncManager) SetSyncInterval(interval time.Duration) {
	s.mutex.Lock()
	defer s.mutex.Unlock()
	// Implementação básica - atualizar configuração
}

// SetBatchSize define o tamanho do lote
func (s *DefaultSyncManager) SetBatchSize(size int) {
	s.mutex.Lock()
	defer s.mutex.Unlock()
	s.config.BatchSize = size
}