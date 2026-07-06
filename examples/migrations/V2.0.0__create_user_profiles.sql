-- Example Versioned Migration: User Profiles
-- @file V2.0.0__create_user_profiles.sql
-- @description Creates user profiles table with extended user information
-- @author Admilson B. F. Cossa
-- @date 2025-01-28
-- @path pkg/core/database/migrations/examples/migrations/V2.0.0__create_user_profiles.sql

-- Create user_profiles table
CREATE TABLE user_profiles (
    id BIGINT AUTO_INCREMENT PRIMARY KEY,
    user_id BIGINT NOT NULL,
    first_name VARCHAR(100),
    last_name VARCHAR(100),
    display_name VARCHAR(200),
    bio TEXT,
    avatar_url VARCHAR(500),
    cover_image_url VARCHAR(500),
    website VARCHAR(500),
    location VARCHAR(200),
    timezone VARCHAR(50) DEFAULT 'UTC',
    language VARCHAR(10) DEFAULT 'en',
    date_of_birth DATE,
    gender ENUM('male', 'female', 'other', 'prefer_not_to_say'),
    phone VARCHAR(20),
    address TEXT,
    city VARCHAR(100),
    state VARCHAR(100),
    country VARCHAR(100),
    postal_code VARCHAR(20),
    
    -- Social media links
    twitter_handle VARCHAR(100),
    linkedin_url VARCHAR(500),
    github_username VARCHAR(100),
    facebook_url VARCHAR(500),
    instagram_handle VARCHAR(100),
    
    -- Professional information
    job_title VARCHAR(200),
    company VARCHAR(200),
    industry VARCHAR(100),
    experience_years INT,
    skills JSON,
    education JSON,
    certifications JSON,
    
    -- Privacy settings
    is_profile_public BOOLEAN DEFAULT TRUE,
    show_email BOOLEAN DEFAULT FALSE,
    show_phone BOOLEAN DEFAULT FALSE,
    show_location BOOLEAN DEFAULT TRUE,
    show_social_links BOOLEAN DEFAULT TRUE,
    
    -- Notification preferences
    email_notifications BOOLEAN DEFAULT TRUE,
    push_notifications BOOLEAN DEFAULT TRUE,
    sms_notifications BOOLEAN DEFAULT FALSE,
    marketing_emails BOOLEAN DEFAULT FALSE,
    
    -- Activity tracking
    last_login_at TIMESTAMP NULL,
    last_activity_at TIMESTAMP NULL,
    login_count BIGINT DEFAULT 0,
    profile_views BIGINT DEFAULT 0,
    
    -- Verification status
    is_verified BOOLEAN DEFAULT FALSE,
    verified_at TIMESTAMP NULL,
    verified_by BIGINT NULL,
    
    -- Timestamps
    created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
    updated_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP ON UPDATE CURRENT_TIMESTAMP,
    
    -- Foreign key constraint
    CONSTRAINT fk_user_profiles_user FOREIGN KEY (user_id) REFERENCES users(id) ON DELETE CASCADE,
    CONSTRAINT fk_user_profiles_verifier FOREIGN KEY (verified_by) REFERENCES users(id) ON DELETE SET NULL,
    
    -- Unique constraint
    UNIQUE KEY uk_user_profiles_user (user_id)
);

-- Create indexes for performance
CREATE INDEX idx_user_profiles_display_name ON user_profiles(display_name);
CREATE INDEX idx_user_profiles_location ON user_profiles(location);
CREATE INDEX idx_user_profiles_company ON user_profiles(company);
CREATE INDEX idx_user_profiles_industry ON user_profiles(industry);
CREATE INDEX idx_user_profiles_verified ON user_profiles(is_verified);
CREATE INDEX idx_user_profiles_public ON user_profiles(is_profile_public);
CREATE INDEX idx_user_profiles_last_activity ON user_profiles(last_activity_at);

-- Composite indexes
CREATE INDEX idx_user_profiles_location_public ON user_profiles(location, is_profile_public);
CREATE INDEX idx_user_profiles_industry_experience ON user_profiles(industry, experience_years);
CREATE INDEX idx_user_profiles_verified_public ON user_profiles(is_verified, is_profile_public);

-- Full-text search index for bio and professional information
ALTER TABLE user_profiles ADD FULLTEXT(bio, job_title, company, industry);

-- Add check constraints
ALTER TABLE user_profiles ADD CONSTRAINT chk_user_profiles_experience_years 
CHECK (experience_years IS NULL OR (experience_years >= 0 AND experience_years <= 70));

ALTER TABLE user_profiles ADD CONSTRAINT chk_user_profiles_login_count 
CHECK (login_count >= 0);

ALTER TABLE user_profiles ADD CONSTRAINT chk_user_profiles_profile_views 
CHECK (profile_views >= 0);

ALTER TABLE user_profiles ADD CONSTRAINT chk_user_profiles_phone_format 
CHECK (phone IS NULL OR phone REGEXP '^[+]?[0-9\s\-\(\)]{7,20}$');

-- Create user_profile_settings table for additional settings
CREATE TABLE user_profile_settings (
    id BIGINT AUTO_INCREMENT PRIMARY KEY,
    user_id BIGINT NOT NULL,
    setting_key VARCHAR(100) NOT NULL,
    setting_value TEXT,
    setting_type ENUM('string', 'number', 'boolean', 'json') DEFAULT 'string',
    is_encrypted BOOLEAN DEFAULT FALSE,
    created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
    updated_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP ON UPDATE CURRENT_TIMESTAMP,
    
    -- Foreign key constraint
    CONSTRAINT fk_user_profile_settings_user FOREIGN KEY (user_id) REFERENCES users(id) ON DELETE CASCADE,
    
    -- Unique constraint for user-setting combination
    UNIQUE KEY uk_user_profile_settings_user_key (user_id, setting_key)
);

-- Create indexes for user_profile_settings
CREATE INDEX idx_user_profile_settings_user ON user_profile_settings(user_id);
CREATE INDEX idx_user_profile_settings_key ON user_profile_settings(setting_key);
CREATE INDEX idx_user_profile_settings_type ON user_profile_settings(setting_type);

-- Create user_activities table for tracking user actions
CREATE TABLE user_activities (
    id BIGINT AUTO_INCREMENT PRIMARY KEY,
    user_id BIGINT NOT NULL,
    activity_type VARCHAR(50) NOT NULL,
    activity_data JSON,
    ip_address VARCHAR(45),
    user_agent VARCHAR(500),
    session_id VARCHAR(100),
    created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
    
    -- Foreign key constraint
    CONSTRAINT fk_user_activities_user FOREIGN KEY (user_id) REFERENCES users(id) ON DELETE CASCADE
);

-- Create indexes for user_activities
CREATE INDEX idx_user_activities_user ON user_activities(user_id);
CREATE INDEX idx_user_activities_type ON user_activities(activity_type);
CREATE INDEX idx_user_activities_created ON user_activities(created_at);
CREATE INDEX idx_user_activities_session ON user_activities(session_id);

-- Composite indexes for user_activities
CREATE INDEX idx_user_activities_user_type ON user_activities(user_id, activity_type);
CREATE INDEX idx_user_activities_user_created ON user_activities(user_id, created_at);

-- Create trigger to update user_profiles.last_activity_at when user_activities is inserted
DELIMITER //
CREATE TRIGGER tr_user_activities_update_last_activity 
AFTER INSERT ON user_activities
FOR EACH ROW
BEGIN
    UPDATE user_profiles 
    SET last_activity_at = NEW.created_at 
    WHERE user_id = NEW.user_id;
END//
DELIMITER ;

-- Create trigger to update user_profiles.profile_views when profile is viewed
DELIMITER //
CREATE TRIGGER tr_user_activities_update_profile_views 
AFTER INSERT ON user_activities
FOR EACH ROW
BEGIN
    IF NEW.activity_type = 'profile_view' THEN
        UPDATE user_profiles 
        SET profile_views = profile_views + 1 
        WHERE user_id = JSON_UNQUOTE(JSON_EXTRACT(NEW.activity_data, '$.viewed_user_id'));
    END IF;
END//
DELIMITER ;

-- Insert default profile settings
INSERT INTO user_profile_settings (user_id, setting_key, setting_value, setting_type)
SELECT 
    id,
    'theme',
    'light',
    'string'
FROM users;

INSERT INTO user_profile_settings (user_id, setting_key, setting_value, setting_type)
SELECT 
    id,
    'posts_per_page',
    '10',
    'number'
FROM users;

INSERT INTO user_profile_settings (user_id, setting_key, setting_value, setting_type)
SELECT 
    id,
    'auto_save_drafts',
    'true',
    'boolean'
FROM users;

-- Create view for complete user information
CREATE VIEW user_complete_profiles AS
SELECT 
    u.id,
    u.username,
    u.email,
    u.is_active,
    u.email_verified_at,
    u.created_at as user_created_at,
    u.updated_at as user_updated_at,
    
    p.first_name,
    p.last_name,
    p.display_name,
    p.bio,
    p.avatar_url,
    p.cover_image_url,
    p.website,
    p.location,
    p.timezone,
    p.language,
    p.date_of_birth,
    p.gender,
    p.phone,
    p.job_title,
    p.company,
    p.industry,
    p.experience_years,
    p.skills,
    p.education,
    p.certifications,
    p.is_profile_public,
    p.is_verified,
    p.verified_at,
    p.last_login_at,
    p.last_activity_at,
    p.login_count,
    p.profile_views,
    p.created_at as profile_created_at,
    p.updated_at as profile_updated_at
FROM users u
LEFT JOIN user_profiles p ON u.id = p.user_id
WHERE u.deleted_at IS NULL;

-- Add comments for documentation
ALTER TABLE user_profiles COMMENT = 'Extended user profile information and preferences';
ALTER TABLE user_profile_settings COMMENT = 'Flexible key-value settings for user profiles';
ALTER TABLE user_activities COMMENT = 'User activity tracking and audit log';