package broker

import (
	"context"
	"encoding/json"
	"fmt"
	"strconv"
	"sync"
	"time"

	"github.com/google/uuid"
	"github.com/gorelov-m-v/gophprofile/internal/domain"
	amqp "github.com/rabbitmq/amqp091-go"
)

const (
	UploadRoutingKey = "avatar.uploaded"
	DeleteRoutingKey = "avatar.deleted"
	UploadQueue      = "avatars.uploads"
	DeleteQueue      = "avatars.deletes"
	retryHeader      = "x-retry-count"
)

type Publisher interface {
	PublishUpload(ctx context.Context, event domain.AvatarUploadEvent) error
	PublishDelete(ctx context.Context, event domain.AvatarDeleteEvent) error
	Ping() error
	Close() error
}

type RabbitMQ struct {
	conn     *amqp.Connection
	channel  *amqp.Channel
	exchange string
	mu       sync.Mutex
}

func NewRabbitMQ(url, exchange string) (*RabbitMQ, error) {
	conn, err := amqp.Dial(url)
	if err != nil {
		return nil, fmt.Errorf("dial rabbitmq: %w", err)
	}
	ch, err := conn.Channel()
	if err != nil {
		_ = conn.Close()
		return nil, fmt.Errorf("open channel: %w", err)
	}
	r := &RabbitMQ{conn: conn, channel: ch, exchange: exchange}
	if err := r.DeclareTopology(); err != nil {
		_ = r.Close()
		return nil, err
	}
	return r, nil
}

func (r *RabbitMQ) DeclareTopology() error {
	r.mu.Lock()
	defer r.mu.Unlock()

	if err := r.channel.ExchangeDeclare(r.exchange, "direct", true, false, false, false, nil); err != nil {
		return fmt.Errorf("declare exchange: %w", err)
	}
	if err := r.channel.Qos(4, 0, false); err != nil {
		return fmt.Errorf("set qos: %w", err)
	}
	if _, err := r.channel.QueueDeclare(UploadQueue, true, false, false, false, nil); err != nil {
		return fmt.Errorf("declare upload queue: %w", err)
	}
	if err := r.channel.QueueBind(UploadQueue, UploadRoutingKey, r.exchange, false, nil); err != nil {
		return fmt.Errorf("bind upload queue: %w", err)
	}
	if _, err := r.channel.QueueDeclare(DeleteQueue, true, false, false, false, nil); err != nil {
		return fmt.Errorf("declare delete queue: %w", err)
	}
	if err := r.channel.QueueBind(DeleteQueue, DeleteRoutingKey, r.exchange, false, nil); err != nil {
		return fmt.Errorf("bind delete queue: %w", err)
	}
	return nil
}

func (r *RabbitMQ) PublishUpload(ctx context.Context, event domain.AvatarUploadEvent) error {
	event.MessageID = ensureMessageID(event.MessageID)
	return r.publish(ctx, UploadRoutingKey, event.MessageID, event)
}

func (r *RabbitMQ) PublishDelete(ctx context.Context, event domain.AvatarDeleteEvent) error {
	event.MessageID = ensureMessageID(event.MessageID)
	return r.publish(ctx, DeleteRoutingKey, event.MessageID, event)
}

func (r *RabbitMQ) publish(ctx context.Context, routingKey, messageID string, event any) error {
	body, err := json.Marshal(event)
	if err != nil {
		return fmt.Errorf("marshal event: %w", err)
	}
	messageID = ensureMessageID(messageID)

	r.mu.Lock()
	defer r.mu.Unlock()
	if r.channel == nil {
		return fmt.Errorf("rabbitmq channel is not initialized")
	}
	if err := r.channel.PublishWithContext(ctx, r.exchange, routingKey, false, false, amqp.Publishing{
		ContentType:   "application/json",
		DeliveryMode:  amqp.Persistent,
		MessageId:     messageID,
		CorrelationId: uuid.NewString(),
		Headers:       amqp.Table{retryHeader: int32(0)},
		Body:          body,
	}); err != nil {
		return fmt.Errorf("publish %s: %w", routingKey, err)
	}
	return nil
}

func ensureMessageID(messageID string) string {
	if messageID != "" {
		return messageID
	}
	return uuid.NewString()
}

func (r *RabbitMQ) Ping() error {
	r.mu.Lock()
	defer r.mu.Unlock()

	if r.conn == nil || r.conn.IsClosed() {
		return fmt.Errorf("rabbitmq connection is closed")
	}
	if r.channel == nil || r.channel.IsClosed() {
		return fmt.Errorf("rabbitmq channel is closed")
	}
	return nil
}

func (r *RabbitMQ) Close() error {
	r.mu.Lock()
	defer r.mu.Unlock()

	var first error
	if r.channel != nil {
		if err := r.channel.Close(); err != nil && first == nil {
			first = err
		}
	}
	if r.conn != nil {
		if err := r.conn.Close(); err != nil && first == nil {
			first = err
		}
	}
	return first
}

type UploadDelivery struct {
	Context    context.Context
	Event      domain.AvatarUploadEvent
	RetryCount int
	Ack        func() error
	Nack       func(requeue bool) error
}

type DeleteDelivery struct {
	Context    context.Context
	Event      domain.AvatarDeleteEvent
	RetryCount int
	Ack        func() error
	Nack       func(requeue bool) error
}

func (r *RabbitMQ) ConsumeUploads(ctx context.Context) (<-chan UploadDelivery, error) {
	r.mu.Lock()
	if r.channel == nil {
		r.mu.Unlock()
		return nil, fmt.Errorf("rabbitmq channel is not initialized")
	}
	deliveries, err := r.channel.ConsumeWithContext(ctx, UploadQueue, "", false, false, false, false, nil)
	r.mu.Unlock()
	if err != nil {
		return nil, fmt.Errorf("consume uploads: %w", err)
	}
	out := make(chan UploadDelivery)
	go func() {
		defer close(out)
		for msg := range deliveries {
			msg := msg
			var event domain.AvatarUploadEvent
			if err := json.Unmarshal(msg.Body, &event); err != nil {
				_ = msg.Nack(false, false)
				continue
			}
			delivery := UploadDelivery{
				Context:    ctx,
				Event:      event,
				RetryCount: retryCount(msg.Headers),
				Ack:        func() error { return msg.Ack(false) },
				Nack:       func(requeue bool) error { return msg.Nack(false, requeue) },
			}
			select {
			case out <- delivery:
			case <-ctx.Done():
				return
			}
		}
	}()
	return out, nil
}

func (r *RabbitMQ) ConsumeDeletes(ctx context.Context) (<-chan DeleteDelivery, error) {
	r.mu.Lock()
	if r.channel == nil {
		r.mu.Unlock()
		return nil, fmt.Errorf("rabbitmq channel is not initialized")
	}
	deliveries, err := r.channel.ConsumeWithContext(ctx, DeleteQueue, "", false, false, false, false, nil)
	r.mu.Unlock()
	if err != nil {
		return nil, fmt.Errorf("consume deletes: %w", err)
	}
	out := make(chan DeleteDelivery)
	go func() {
		defer close(out)
		for msg := range deliveries {
			msg := msg
			var event domain.AvatarDeleteEvent
			if err := json.Unmarshal(msg.Body, &event); err != nil {
				_ = msg.Nack(false, false)
				continue
			}
			delivery := DeleteDelivery{
				Context:    ctx,
				Event:      event,
				RetryCount: retryCount(msg.Headers),
				Ack:        func() error { return msg.Ack(false) },
				Nack:       func(requeue bool) error { return msg.Nack(false, requeue) },
			}
			select {
			case out <- delivery:
			case <-ctx.Done():
				return
			}
		}
	}()
	return out, nil
}

func RetryBackoff(attempt int) time.Duration {
	if attempt < 0 {
		attempt = 0
	}
	if attempt > 6 {
		attempt = 6
	}
	return time.Duration(1<<attempt) * 200 * time.Millisecond
}

func retryCount(headers amqp.Table) int {
	if headers == nil {
		return 0
	}
	value, ok := headers[retryHeader]
	if !ok {
		return 0
	}
	switch v := value.(type) {
	case int:
		return v
	case int32:
		return int(v)
	case int64:
		return int(v)
	case string:
		parsed, _ := strconv.Atoi(v)
		return parsed
	default:
		return 0
	}
}
