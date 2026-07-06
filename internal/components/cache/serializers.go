/**
 * @file serializers.go
 * @description Cache serializers implementation for different data formats
 * @author Admilson B. F. Cossa
 * @date 2024-12-30
 * @path pkg/core/database/migrations/internal/components/cache/serializers.go
 */

package cache

import (
	"bytes"
	"compress/gzip"
	"context"
	"encoding/gob"
	"encoding/json"
	"fmt"
	"io"
	"reflect"
	"sync"
)

// JSONSerializer implements JSON serialization
type JSONSerializer struct {
	config *SerializerConfig
	mutex  sync.RWMutex
}

// SerializerConfig holds serializer configuration
type SerializerConfig struct {
	CompressionEnabled bool
	CompressionLevel   int
	PrettyPrint        bool
	TypeRegistry       map[string]reflect.Type
}

// NewJSONSerializer creates a new JSON serializer
func NewJSONSerializer(config *SerializerConfig) *JSONSerializer {
	if config == nil {
		config = &SerializerConfig{
			CompressionEnabled: false,
			CompressionLevel:   gzip.DefaultCompression,
			PrettyPrint:        false,
			TypeRegistry:       make(map[string]reflect.Type),
		}
	}

	return &JSONSerializer{
		config: config,
	}
}

// Serialize converts value to bytes (interface method)
func (s *JSONSerializer) Serialize(obj interface{}) ([]byte, error) {
	return s.SerializeWithContext(context.Background(), obj)
}

// Deserialize converts bytes to value (interface method)
func (s *JSONSerializer) Deserialize(data []byte, target interface{}) error {
	return s.DeserializeWithContext(context.Background(), data, target)
}

// GetContentType returns the content type
func (s *JSONSerializer) GetContentType() string {
	return "application/json"
}

// GetSerializationType returns the serialization type
func (s *JSONSerializer) GetSerializationType() SerializationType {
	return SerializationTypeJSON
}

// GetConfig returns the serializer configuration
func (s *JSONSerializer) GetConfig() interface{} {
	s.mutex.RLock()
	defer s.mutex.RUnlock()

	configCopy := *s.config
	configCopy.TypeRegistry = make(map[string]reflect.Type)
	for k, v := range s.config.TypeRegistry {
		configCopy.TypeRegistry[k] = v
	}

	return &configCopy
}

// SerializeWithContext converts value to bytes with context
func (s *JSONSerializer) SerializeWithContext(ctx context.Context, value interface{}) ([]byte, error) {
	s.mutex.RLock()
	defer s.mutex.RUnlock()

	var data []byte
	var err error

	if s.config.PrettyPrint {
		data, err = json.MarshalIndent(value, "", "  ")
	} else {
		data, err = json.Marshal(value)
	}

	if err != nil {
		return nil, fmt.Errorf("failed to marshal JSON: %w", err)
	}

	if s.config.CompressionEnabled {
		return s.compress(data)
	}

	return data, nil
}

// DeserializeWithContext converts bytes to value with context
func (s *JSONSerializer) DeserializeWithContext(ctx context.Context, data []byte, target interface{}) error {
	s.mutex.RLock()
	defer s.mutex.RUnlock()

	var err error

	if s.config.CompressionEnabled {
		data, err = s.decompress(data)
		if err != nil {
			return fmt.Errorf("failed to decompress data: %w", err)
		}
	}

	if err := json.Unmarshal(data, target); err != nil {
		return fmt.Errorf("failed to unmarshal JSON: %w", err)
	}

	return nil
}

// UpdateConfig updates the serializer configuration
func (s *JSONSerializer) UpdateConfig(ctx context.Context, config interface{}) error {
	s.mutex.Lock()
	defer s.mutex.Unlock()

	if newConfig, ok := config.(*SerializerConfig); ok {
		s.config = newConfig
		return nil
	}

	return fmt.Errorf("invalid config type for JSON serializer")
}

func (s *JSONSerializer) compress(data []byte) ([]byte, error) {
	var buf bytes.Buffer
	writer, err := gzip.NewWriterLevel(&buf, s.config.CompressionLevel)
	if err != nil {
		return nil, err
	}

	if _, err := writer.Write(data); err != nil {
		return nil, err
	}

	if err := writer.Close(); err != nil {
		return nil, err
	}

	return buf.Bytes(), nil
}

func (s *JSONSerializer) decompress(data []byte) ([]byte, error) {
	reader, err := gzip.NewReader(bytes.NewReader(data))
	if err != nil {
		return nil, err
	}
	defer reader.Close()

	return io.ReadAll(reader)
}

// BinarySerializer implements binary serialization using Gob
type BinarySerializer struct {
	config       *SerializerConfig
	encoder      *gob.Encoder
	decoder      *gob.Decoder
	encodeBuffer *bytes.Buffer
	decodeBuffer *bytes.Buffer
	mutex        sync.RWMutex
}

// NewBinarySerializer creates a new binary serializer
func NewBinarySerializer(config *SerializerConfig) *BinarySerializer {
	if config == nil {
		config = &SerializerConfig{
			CompressionEnabled: true,
			CompressionLevel:   gzip.DefaultCompression,
			TypeRegistry:       make(map[string]reflect.Type),
		}
	}

	encodeBuffer := &bytes.Buffer{}
	decodeBuffer := &bytes.Buffer{}

	serializer := &BinarySerializer{
		config:       config,
		encodeBuffer: encodeBuffer,
		decodeBuffer: decodeBuffer,
		encoder:      gob.NewEncoder(encodeBuffer),
		decoder:      gob.NewDecoder(decodeBuffer),
	}

	// Register common types
	serializer.registerCommonTypes()

	return serializer
}

// Serialize converts value to bytes
func (s *BinarySerializer) Serialize(value interface{}) ([]byte, error) {
	s.mutex.Lock()
	defer s.mutex.Unlock()

	// Reset buffer
	s.encodeBuffer.Reset()

	// Register type if needed
	s.registerType(value)

	// Encode value
	if err := s.encoder.Encode(value); err != nil {
		return nil, fmt.Errorf("failed to encode binary data: %w", err)
	}

	data := s.encodeBuffer.Bytes()

	if s.config.CompressionEnabled {
		return s.compress(data)
	}

	return data, nil
}

// Deserialize converts bytes to value
func (s *BinarySerializer) Deserialize(data []byte, target interface{}) error {
	s.mutex.Lock()
	defer s.mutex.Unlock()

	var err error

	if s.config.CompressionEnabled {
		data, err = s.decompress(data)
		if err != nil {
			return fmt.Errorf("failed to decompress data: %w", err)
		}
	}

	// Reset buffer and set data
	s.decodeBuffer.Reset()
	s.decodeBuffer.Write(data)

	// Register type if needed
	s.registerType(target)

	// Decode value
	if err := s.decoder.Decode(target); err != nil {
		return fmt.Errorf("failed to decode binary data: %w", err)
	}

	return nil
}

// GetSerializationType returns the serialization type
func (s *BinarySerializer) GetSerializationType() SerializationType {
	return SerializationTypeBinary
}

// GetContentType returns the content type
func (s *BinarySerializer) GetContentType() string {
	return "application/octet-stream"
}

// GetConfig returns the serializer configuration
func (s *BinarySerializer) GetConfig() interface{} {
	s.mutex.RLock()
	defer s.mutex.RUnlock()

	configCopy := *s.config
	configCopy.TypeRegistry = make(map[string]reflect.Type)
	for k, v := range s.config.TypeRegistry {
		configCopy.TypeRegistry[k] = v
	}

	return &configCopy
}

// UpdateConfig updates the serializer configuration
func (s *BinarySerializer) UpdateConfig(ctx context.Context, config interface{}) error {
	s.mutex.Lock()
	defer s.mutex.Unlock()

	if newConfig, ok := config.(*SerializerConfig); ok {
		s.config = newConfig
		return nil
	}

	return fmt.Errorf("invalid config type for binary serializer")
}

func (s *BinarySerializer) registerType(value interface{}) {
	valueType := reflect.TypeOf(value)
	if valueType == nil {
		return
	}

	// Handle pointers
	if valueType.Kind() == reflect.Ptr {
		valueType = valueType.Elem()
	}

	typeName := valueType.String()
	if _, exists := s.config.TypeRegistry[typeName]; !exists {
		gob.Register(value)
		s.config.TypeRegistry[typeName] = valueType
	}
}

func (s *BinarySerializer) registerCommonTypes() {
	// Register common types
	gob.Register(map[string]interface{}{})
	gob.Register([]interface{}{})
	gob.Register("")
	gob.Register(0)
	gob.Register(int64(0))
	gob.Register(float64(0))
	gob.Register(true)
}

func (s *BinarySerializer) compress(data []byte) ([]byte, error) {
	var buf bytes.Buffer
	writer, err := gzip.NewWriterLevel(&buf, s.config.CompressionLevel)
	if err != nil {
		return nil, err
	}

	if _, err := writer.Write(data); err != nil {
		return nil, err
	}

	if err := writer.Close(); err != nil {
		return nil, err
	}

	return buf.Bytes(), nil
}

func (s *BinarySerializer) decompress(data []byte) ([]byte, error) {
	reader, err := gzip.NewReader(bytes.NewReader(data))
	if err != nil {
		return nil, err
	}
	defer reader.Close()

	return io.ReadAll(reader)
}

// GobSerializer implements Gob serialization (optimized binary format)
type GobSerializer struct {
	config       *SerializerConfig
	typeRegistry map[string]bool
	mutex        sync.RWMutex
}

// NewGobSerializer creates a new Gob serializer
func NewGobSerializer(config *SerializerConfig) *GobSerializer {
	if config == nil {
		config = &SerializerConfig{
			CompressionEnabled: true,
			CompressionLevel:   gzip.DefaultCompression,
			TypeRegistry:       make(map[string]reflect.Type),
		}
	}

	serializer := &GobSerializer{
		config:       config,
		typeRegistry: make(map[string]bool),
	}

	// Register common types
	serializer.registerCommonTypes()

	return serializer
}

// Serialize converts value to bytes
func (s *GobSerializer) Serialize(value interface{}) ([]byte, error) {
	s.mutex.Lock()
	defer s.mutex.Unlock()

	// Register type if needed
	s.registerType(value)

	var buf bytes.Buffer
	encoder := gob.NewEncoder(&buf)

	if err := encoder.Encode(value); err != nil {
		return nil, fmt.Errorf("failed to encode gob data: %w", err)
	}

	data := buf.Bytes()

	if s.config.CompressionEnabled {
		return s.compress(data)
	}

	return data, nil
}

// Deserialize converts bytes back to an object
func (s *GobSerializer) Deserialize(data []byte, target interface{}) error {
	s.mutex.Lock()
	defer s.mutex.Unlock()

	var err error

	if s.config.CompressionEnabled {
		data, err = s.decompress(data)
		if err != nil {
			return fmt.Errorf("failed to decompress data: %w", err)
		}
	}

	// Register type if needed
	s.registerType(target)

	buf := bytes.NewBuffer(data)
	decoder := gob.NewDecoder(buf)

	if err := decoder.Decode(target); err != nil {
		return fmt.Errorf("failed to decode gob data: %w", err)
	}

	return nil
}

// GetSerializationType returns the serialization type
func (s *GobSerializer) GetSerializationType() SerializationType {
	return SerializationTypeGOB
}

// GetContentType returns the content type
func (s *GobSerializer) GetContentType() string {
	return "application/gob"
}

// GetConfig returns the serializer configuration
func (s *GobSerializer) GetConfig() interface{} {
	s.mutex.RLock()
	defer s.mutex.RUnlock()

	configCopy := *s.config
	configCopy.TypeRegistry = make(map[string]reflect.Type)
	for k, v := range s.config.TypeRegistry {
		configCopy.TypeRegistry[k] = v
	}

	return &configCopy
}

// UpdateConfig updates the serializer configuration
func (s *GobSerializer) UpdateConfig(ctx context.Context, config interface{}) error {
	s.mutex.Lock()
	defer s.mutex.Unlock()

	if newConfig, ok := config.(*SerializerConfig); ok {
		s.config = newConfig
		return nil
	}

	return fmt.Errorf("invalid config type for gob serializer")
}

func (s *GobSerializer) registerType(value interface{}) {
	valueType := reflect.TypeOf(value)
	if valueType == nil {
		return
	}

	// Handle pointers
	if valueType.Kind() == reflect.Ptr {
		valueType = valueType.Elem()
	}

	typeName := valueType.String()
	if !s.typeRegistry[typeName] {
		gob.Register(value)
		s.typeRegistry[typeName] = true
		s.config.TypeRegistry[typeName] = valueType
	}
}

func (s *GobSerializer) registerCommonTypes() {
	// Register common types
	commonTypes := []interface{}{
		map[string]interface{}{},
		[]interface{}{},
		"",
		0,
		int64(0),
		float64(0),
		true,
		[]byte{},
		map[string]string{},
		[]string{},
	}

	for _, t := range commonTypes {
		s.registerType(t)
	}
}

func (s *GobSerializer) compress(data []byte) ([]byte, error) {
	var buf bytes.Buffer
	writer, err := gzip.NewWriterLevel(&buf, s.config.CompressionLevel)
	if err != nil {
		return nil, err
	}

	if _, err := writer.Write(data); err != nil {
		return nil, err
	}

	if err := writer.Close(); err != nil {
		return nil, err
	}

	return buf.Bytes(), nil
}

func (s *GobSerializer) decompress(data []byte) ([]byte, error) {
	reader, err := gzip.NewReader(bytes.NewReader(data))
	if err != nil {
		return nil, err
	}
	defer reader.Close()

	return io.ReadAll(reader)
}

// MessagePackSerializer implements MessagePack serialization (placeholder)
type MessagePackSerializer struct {
	config *SerializerConfig
	mutex  sync.RWMutex
}

// NewMessagePackSerializer creates a new MessagePack serializer
func NewMessagePackSerializer(config *SerializerConfig) *MessagePackSerializer {
	if config == nil {
		config = &SerializerConfig{
			CompressionEnabled: false,
			CompressionLevel:   gzip.DefaultCompression,
			TypeRegistry:       make(map[string]reflect.Type),
		}
	}

	return &MessagePackSerializer{
		config: config,
	}
}

// Serialize converts value to bytes (placeholder implementation)
func (s *MessagePackSerializer) Serialize(value interface{}) ([]byte, error) {
	// This would use a MessagePack library like github.com/vmihailenco/msgpack/v5
	// For now, fall back to JSON
	jsonSerializer := NewJSONSerializer(s.config)
	return jsonSerializer.Serialize(value)
}

// Deserialize converts bytes to value (placeholder implementation)
func (s *MessagePackSerializer) Deserialize(data []byte, target interface{}) error {
	// This would use a MessagePack library like github.com/vmihailenco/msgpack/v5
	// For now, fall back to JSON
	jsonSerializer := NewJSONSerializer(s.config)
	return jsonSerializer.Deserialize(data, target)
}

// GetSerializationType returns the serialization type
func (s *MessagePackSerializer) GetSerializationType() SerializationType {
	return SerializationTypeMsgPack
}

// GetContentType returns the content type
func (s *MessagePackSerializer) GetContentType() string {
	return "application/msgpack"
}

// GetConfig returns the serializer configuration
func (s *MessagePackSerializer) GetConfig() interface{} {
	s.mutex.RLock()
	defer s.mutex.RUnlock()

	configCopy := *s.config
	return &configCopy
}

// UpdateConfig updates the serializer configuration
func (s *MessagePackSerializer) UpdateConfig(ctx context.Context, config interface{}) error {
	s.mutex.Lock()
	defer s.mutex.Unlock()

	if newConfig, ok := config.(*SerializerConfig); ok {
		s.config = newConfig
		return nil
	}

	return fmt.Errorf("invalid config type for MessagePack serializer")
}

// SerializerFactory creates serializers based on type
type SerializerFactory struct {
	serializers map[SerializationType]CacheSerializer
	mutex       sync.RWMutex
}

// NewSerializerFactory creates a new serializer factory
func NewSerializerFactory() *SerializerFactory {
	factory := &SerializerFactory{
		serializers: make(map[SerializationType]CacheSerializer),
	}

	// Register default serializers
	factory.RegisterSerializer(SerializationTypeJSON, NewJSONSerializer(nil))
	factory.RegisterSerializer(SerializationTypeBinary, NewBinarySerializer(nil))
	factory.RegisterSerializer(SerializationTypeGOB, NewGobSerializer(nil))
	factory.RegisterSerializer(SerializationTypeMsgPack, NewMessagePackSerializer(nil))

	return factory
}

// GetSerializer returns a serializer for the specified type
func (f *SerializerFactory) GetSerializer(serializationType SerializationType) (CacheSerializer, error) {
	f.mutex.RLock()
	defer f.mutex.RUnlock()

	serializer, exists := f.serializers[serializationType]
	if !exists {
		return nil, fmt.Errorf("no serializer registered for type: %s", serializationType)
	}

	return serializer, nil
}

// RegisterSerializer registers a serializer for a specific type
func (f *SerializerFactory) RegisterSerializer(serializationType SerializationType, serializer CacheSerializer) {
	f.mutex.Lock()
	defer f.mutex.Unlock()

	f.serializers[serializationType] = serializer
}

// GetAvailableTypes returns all available serialization types
func (f *SerializerFactory) GetAvailableTypes() []SerializationType {
	f.mutex.RLock()
	defer f.mutex.RUnlock()

	types := make([]SerializationType, 0, len(f.serializers))
	for serializationType := range f.serializers {
		types = append(types, serializationType)
	}

	return types
}

// CreateSerializer creates a new serializer with custom configuration
func (f *SerializerFactory) CreateSerializer(serializationType SerializationType, config *SerializerConfig) (CacheSerializer, error) {
	switch serializationType {
	case SerializationTypeJSON:
		return NewJSONSerializer(config), nil
	case SerializationTypeBinary:
		return NewBinarySerializer(config), nil
	case SerializationTypeGOB:
		return NewGobSerializer(config), nil
	case SerializationTypeMsgPack:
		return NewMessagePackSerializer(config), nil
	default:
		return nil, fmt.Errorf("unsupported serialization type: %s", serializationType)
	}
}

// Default serializer factory instance
var DefaultSerializerFactory = NewSerializerFactory()

// Helper functions

// GetDefaultSerializerConfig returns default serializer configuration
func GetDefaultSerializerConfig() *SerializerConfig {
	return &SerializerConfig{
		CompressionEnabled: false,
		CompressionLevel:   gzip.DefaultCompression,
		PrettyPrint:        false,
		TypeRegistry:       make(map[string]reflect.Type),
	}
}

// GetCompressedSerializerConfig returns configuration with compression enabled
func GetCompressedSerializerConfig() *SerializerConfig {
	return &SerializerConfig{
		CompressionEnabled: true,
		CompressionLevel:   gzip.BestCompression,
		PrettyPrint:        false,
		TypeRegistry:       make(map[string]reflect.Type),
	}
}

// GetPrettyJSONSerializerConfig returns configuration for pretty JSON
func GetPrettyJSONSerializerConfig() *SerializerConfig {
	return &SerializerConfig{
		CompressionEnabled: false,
		CompressionLevel:   gzip.DefaultCompression,
		PrettyPrint:        true,
		TypeRegistry:       make(map[string]reflect.Type),
	}
}
