-- Example Versioned Migration: Notifications System
-- @file V3.0.0__create_notifications_system.sql
-- @description Creates comprehensive notifications system with templates and channels
-- @author Admilson B. F. Cossa
-- @date 2025-01-28
-- @path pkg/core/database/migrations/examples/migrations/V3.0.0__create_notifications_system.sql

-- Create notification_templates table
CREATE TABLE notification_templates (
    id BIGINT AUTO_INCREMENT PRIMARY KEY,
    name VARCHAR(100) NOT NULL UNIQUE,
    type VARCHAR(50) NOT NULL,
    channel VARCHAR(20) NOT NULL,
    subject_template TEXT,
    body_template TEXT NOT NULL,
    html_template TEXT,
    variables JSON,
    is_active BOOLEAN DEFAULT TRUE,
    created_by BIGINT,
    created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
    updated_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP ON UPDATE CURRENT_TIMESTAMP,
    
    -- Foreign key constraint
    CONSTRAINT fk_notification_templates_creator FOREIGN KEY (created_by) REFERENCES users(id) ON DELETE SET NULL
);

-- Create indexes for notification_templates
CREATE INDEX idx_notification_templates_type ON notification_templates(type);
CREATE INDEX idx_notification_templates_channel ON notification_templates(channel);
CREATE INDEX idx_notification_templates_active ON notification_templates(is_active);
CREATE INDEX idx_notification_templates_type_channel ON notification_templates(type, channel);

-- Add check constraint for channel
ALTER TABLE notification_templates ADD CONSTRAINT chk_notification_templates_channel 
CHECK (channel IN ('email', 'sms', 'push', 'in_app', 'webhook'));

-- Create notifications table
CREATE TABLE notifications (
    id BIGINT AUTO_INCREMENT PRIMARY KEY,
    user_id BIGINT NOT NULL,
    template_id BIGINT,
    type VARCHAR(50) NOT NULL,
    channel VARCHAR(20) NOT NULL,
    title VARCHAR(255) NOT NULL,
    message TEXT NOT NULL,
    html_content TEXT,
    data JSON,
    
    -- Delivery information
    recipient_email VARCHAR(255),
    recipient_phone VARCHAR(20),
    recipient_device_token VARCHAR(500),
    webhook_url VARCHAR(500),
    
    -- Status tracking
    status ENUM('pending', 'sent', 'delivered', 'failed', 'cancelled') DEFAULT 'pending',
    sent_at TIMESTAMP NULL,
    delivered_at TIMESTAMP NULL,
    failed_at TIMESTAMP NULL,
    failure_reason TEXT,
    retry_count INT DEFAULT 0,
    max_retries INT DEFAULT 3,
    
    -- Read tracking (for in-app notifications)
    is_read BOOLEAN DEFAULT FALSE,
    read_at TIMESTAMP NULL,
    
    -- Priority and scheduling
    priority ENUM('low', 'normal', 'high', 'urgent') DEFAULT 'normal',
    scheduled_at TIMESTAMP NULL,
    expires_at TIMESTAMP NULL,
    
    -- Grouping and batching
    group_key VARCHAR(100),
    batch_id VARCHAR(100),
    
    -- Metadata
    source VARCHAR(50),
    source_id VARCHAR(100),
    tags JSON,
    
    -- Timestamps
    created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
    updated_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP ON UPDATE CURRENT_TIMESTAMP,
    
    -- Foreign key constraints
    CONSTRAINT fk_notifications_user FOREIGN KEY (user_id) REFERENCES users(id) ON DELETE CASCADE,
    CONSTRAINT fk_notifications_template FOREIGN KEY (template_id) REFERENCES notification_templates(id) ON DELETE SET NULL
);

-- Create indexes for notifications
CREATE INDEX idx_notifications_user ON notifications(user_id);
CREATE INDEX idx_notifications_template ON notifications(template_id);
CREATE INDEX idx_notifications_type ON notifications(type);
CREATE INDEX idx_notifications_channel ON notifications(channel);
CREATE INDEX idx_notifications_status ON notifications(status);
CREATE INDEX idx_notifications_priority ON notifications(priority);
CREATE INDEX idx_notifications_scheduled ON notifications(scheduled_at);
CREATE INDEX idx_notifications_created ON notifications(created_at);
CREATE INDEX idx_notifications_read ON notifications(is_read);
CREATE INDEX idx_notifications_group ON notifications(group_key);
CREATE INDEX idx_notifications_batch ON notifications(batch_id);
CREATE INDEX idx_notifications_source ON notifications(source, source_id);

-- Composite indexes for common queries
CREATE INDEX idx_notifications_user_status ON notifications(user_id, status);
CREATE INDEX idx_notifications_user_read ON notifications(user_id, is_read);
CREATE INDEX idx_notifications_user_type ON notifications(user_id, type);
CREATE INDEX idx_notifications_status_scheduled ON notifications(status, scheduled_at);
CREATE INDEX idx_notifications_channel_status ON notifications(channel, status);

-- Add check constraints
ALTER TABLE notifications ADD CONSTRAINT chk_notifications_channel 
CHECK (channel IN ('email', 'sms', 'push', 'in_app', 'webhook'));

ALTER TABLE notifications ADD CONSTRAINT chk_notifications_retry_count 
CHECK (retry_count >= 0 AND retry_count <= max_retries);

ALTER TABLE notifications ADD CONSTRAINT chk_notifications_max_retries 
CHECK (max_retries >= 0 AND max_retries <= 10);

-- Create notification_preferences table
CREATE TABLE notification_preferences (
    id BIGINT AUTO_INCREMENT PRIMARY KEY,
    user_id BIGINT NOT NULL,
    notification_type VARCHAR(50) NOT NULL,
    channel VARCHAR(20) NOT NULL,
    is_enabled BOOLEAN DEFAULT TRUE,
    frequency ENUM('immediate', 'hourly', 'daily', 'weekly', 'never') DEFAULT 'immediate',
    quiet_hours_start TIME,
    quiet_hours_end TIME,
    timezone VARCHAR(50) DEFAULT 'UTC',
    created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
    updated_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP ON UPDATE CURRENT_TIMESTAMP,
    
    -- Foreign key constraint
    CONSTRAINT fk_notification_preferences_user FOREIGN KEY (user_id) REFERENCES users(id) ON DELETE CASCADE,
    
    -- Unique constraint
    UNIQUE KEY uk_notification_preferences_user_type_channel (user_id, notification_type, channel)
);

-- Create indexes for notification_preferences
CREATE INDEX idx_notification_preferences_user ON notification_preferences(user_id);
CREATE INDEX idx_notification_preferences_type ON notification_preferences(notification_type);
CREATE INDEX idx_notification_preferences_channel ON notification_preferences(channel);
CREATE INDEX idx_notification_preferences_enabled ON notification_preferences(is_enabled);

-- Add check constraint for channel
ALTER TABLE notification_preferences ADD CONSTRAINT chk_notification_preferences_channel 
CHECK (channel IN ('email', 'sms', 'push', 'in_app', 'webhook'));

-- Create notification_delivery_log table for detailed tracking
CREATE TABLE notification_delivery_log (
    id BIGINT AUTO_INCREMENT PRIMARY KEY,
    notification_id BIGINT NOT NULL,
    attempt_number INT NOT NULL,
    status ENUM('pending', 'sent', 'delivered', 'failed') NOT NULL,
    response_code VARCHAR(10),
    response_message TEXT,
    provider VARCHAR(50),
    provider_message_id VARCHAR(100),
    delivery_time_ms INT,
    attempted_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
    
    -- Foreign key constraint
    CONSTRAINT fk_notification_delivery_log_notification FOREIGN KEY (notification_id) REFERENCES notifications(id) ON DELETE CASCADE
);

-- Create indexes for notification_delivery_log
CREATE INDEX idx_notification_delivery_log_notification ON notification_delivery_log(notification_id);
CREATE INDEX idx_notification_delivery_log_status ON notification_delivery_log(status);
CREATE INDEX idx_notification_delivery_log_provider ON notification_delivery_log(provider);
CREATE INDEX idx_notification_delivery_log_attempted ON notification_delivery_log(attempted_at);

-- Create notification_subscriptions table for topic-based notifications
CREATE TABLE notification_subscriptions (
    id BIGINT AUTO_INCREMENT PRIMARY KEY,
    user_id BIGINT NOT NULL,
    topic VARCHAR(100) NOT NULL,
    channel VARCHAR(20) NOT NULL,
    is_active BOOLEAN DEFAULT TRUE,
    metadata JSON,
    created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
    updated_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP ON UPDATE CURRENT_TIMESTAMP,
    
    -- Foreign key constraint
    CONSTRAINT fk_notification_subscriptions_user FOREIGN KEY (user_id) REFERENCES users(id) ON DELETE CASCADE,
    
    -- Unique constraint
    UNIQUE KEY uk_notification_subscriptions_user_topic_channel (user_id, topic, channel)
);

-- Create indexes for notification_subscriptions
CREATE INDEX idx_notification_subscriptions_user ON notification_subscriptions(user_id);
CREATE INDEX idx_notification_subscriptions_topic ON notification_subscriptions(topic);
CREATE INDEX idx_notification_subscriptions_channel ON notification_subscriptions(channel);
CREATE INDEX idx_notification_subscriptions_active ON notification_subscriptions(is_active);

-- Add check constraint for channel
ALTER TABLE notification_subscriptions ADD CONSTRAINT chk_notification_subscriptions_channel 
CHECK (channel IN ('email', 'sms', 'push', 'in_app', 'webhook'));

-- Create notification_devices table for push notifications
CREATE TABLE notification_devices (
    id BIGINT AUTO_INCREMENT PRIMARY KEY,
    user_id BIGINT NOT NULL,
    device_token VARCHAR(500) NOT NULL,
    device_type ENUM('ios', 'android', 'web') NOT NULL,
    device_name VARCHAR(100),
    app_version VARCHAR(20),
    os_version VARCHAR(20),
    is_active BOOLEAN DEFAULT TRUE,
    last_used_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
    created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
    updated_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP ON UPDATE CURRENT_TIMESTAMP,
    
    -- Foreign key constraint
    CONSTRAINT fk_notification_devices_user FOREIGN KEY (user_id) REFERENCES users(id) ON DELETE CASCADE,
    
    -- Unique constraint
    UNIQUE KEY uk_notification_devices_token (device_token)
);

-- Create indexes for notification_devices
CREATE INDEX idx_notification_devices_user ON notification_devices(user_id);
CREATE INDEX idx_notification_devices_type ON notification_devices(device_type);
CREATE INDEX idx_notification_devices_active ON notification_devices(is_active);
CREATE INDEX idx_notification_devices_last_used ON notification_devices(last_used_at);

-- Insert default notification templates
INSERT INTO notification_templates (name, type, channel, subject_template, body_template, variables) VALUES
('welcome_email', 'welcome', 'email', 'Welcome to {{app_name}}!', 
 'Hi {{user_name}},\n\nWelcome to {{app_name}}! We''re excited to have you on board.\n\nBest regards,\nThe {{app_name}} Team',
 '["app_name", "user_name"]'),

('password_reset_email', 'password_reset', 'email', 'Reset your password',
 'Hi {{user_name}},\n\nYou requested a password reset. Click the link below to reset your password:\n\n{{reset_link}}\n\nThis link will expire in {{expiry_hours}} hours.\n\nIf you didn''t request this, please ignore this email.',
 '["user_name", "reset_link", "expiry_hours"]'),

('new_comment_notification', 'new_comment', 'in_app', 'New comment on your post',
 '{{commenter_name}} commented on your post "{{post_title}}"',
 '["commenter_name", "post_title", "comment_content"]'),

('post_published_notification', 'post_published', 'push', 'Your post is live!',
 'Your post "{{post_title}}" has been published successfully.',
 '["post_title", "post_url"]'),

('weekly_digest_email', 'weekly_digest', 'email', 'Your weekly digest',
 'Hi {{user_name}},\n\nHere''s what happened this week:\n\n{{digest_content}}\n\nBest regards,\nThe {{app_name}} Team',
 '["user_name", "app_name", "digest_content"]');

-- Insert default notification preferences for existing users
INSERT INTO notification_preferences (user_id, notification_type, channel, is_enabled, frequency)
SELECT 
    u.id,
    'welcome',
    'email',
    TRUE,
    'immediate'
FROM users u;

INSERT INTO notification_preferences (user_id, notification_type, channel, is_enabled, frequency)
SELECT 
    u.id,
    'password_reset',
    'email',
    TRUE,
    'immediate'
FROM users u;

INSERT INTO notification_preferences (user_id, notification_type, channel, is_enabled, frequency)
SELECT 
    u.id,
    'new_comment',
    'in_app',
    TRUE,
    'immediate'
FROM users u;

INSERT INTO notification_preferences (user_id, notification_type, channel, is_enabled, frequency)
SELECT 
    u.id,
    'post_published',
    'push',
    TRUE,
    'immediate'
FROM users u;

INSERT INTO notification_preferences (user_id, notification_type, channel, is_enabled, frequency)
SELECT 
    u.id,
    'weekly_digest',
    'email',
    TRUE,
    'weekly'
FROM users u;

-- Create triggers for automatic notification cleanup
DELIMITER //
CREATE TRIGGER tr_notifications_cleanup_old 
BEFORE INSERT ON notifications
FOR EACH ROW
BEGIN
    -- Delete notifications older than 90 days for the same user
    DELETE FROM notifications 
    WHERE user_id = NEW.user_id 
    AND created_at < DATE_SUB(NOW(), INTERVAL 90 DAY)
    AND is_read = TRUE;
END//
DELIMITER ;

-- Create trigger to update notification status based on delivery log
DELIMITER //
CREATE TRIGGER tr_notification_delivery_log_update_status 
AFTER INSERT ON notification_delivery_log
FOR EACH ROW
BEGIN
    UPDATE notifications 
    SET 
        status = NEW.status,
        sent_at = CASE WHEN NEW.status = 'sent' THEN NEW.attempted_at ELSE sent_at END,
        delivered_at = CASE WHEN NEW.status = 'delivered' THEN NEW.attempted_at ELSE delivered_at END,
        failed_at = CASE WHEN NEW.status = 'failed' THEN NEW.attempted_at ELSE failed_at END,
        retry_count = NEW.attempt_number
    WHERE id = NEW.notification_id;
END//
DELIMITER ;

-- Create views for common notification queries
CREATE VIEW unread_notifications AS
SELECT 
    n.*,
    u.username,
    u.email
FROM notifications n
JOIN users u ON n.user_id = u.id
WHERE n.is_read = FALSE 
AND n.channel = 'in_app'
AND n.status = 'sent'
AND (n.expires_at IS NULL OR n.expires_at > NOW());

CREATE VIEW notification_stats AS
SELECT 
    user_id,
    COUNT(*) as total_notifications,
    SUM(CASE WHEN is_read = FALSE THEN 1 ELSE 0 END) as unread_count,
    SUM(CASE WHEN status = 'delivered' THEN 1 ELSE 0 END) as delivered_count,
    SUM(CASE WHEN status = 'failed' THEN 1 ELSE 0 END) as failed_count,
    MAX(created_at) as last_notification_at
FROM notifications
GROUP BY user_id;

-- Add table comments
ALTER TABLE notification_templates COMMENT = 'Templates for different types of notifications';
ALTER TABLE notifications COMMENT = 'Individual notification records with delivery tracking';
ALTER TABLE notification_preferences COMMENT = 'User preferences for notification types and channels';
ALTER TABLE notification_delivery_log COMMENT = 'Detailed delivery attempt logs for notifications';
ALTER TABLE notification_subscriptions COMMENT = 'Topic-based notification subscriptions';
ALTER TABLE notification_devices COMMENT = 'User devices for push notifications';