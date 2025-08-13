package database

import (
	"fmt"
	"log"
	"os"
	"pocwhisp/models"
	"time"

	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

// Config holds database configuration
type Config struct {
	DSN             string
	MaxOpenConns    int
	MaxIdleConns    int
	ConnMaxLifetime time.Duration
	LogLevel        logger.LogLevel
}

// DefaultConfig returns default database configuration
func DefaultConfig() *Config {
	return &Config{
		DSN:             "pocwhisp.db",
		MaxOpenConns:    25,
		MaxIdleConns:    5,
		ConnMaxLifetime: 5 * time.Minute,
		LogLevel:        logger.Info,
	}
}

// Initialize sets up the database connection and runs migrations
func Initialize(config *Config) (*gorm.DB, error) {
	if config == nil {
		config = DefaultConfig()
	}

	// Configure GORM logger
	gormLogger := logger.New(
		log.New(os.Stdout, "\r\n", log.LstdFlags),
		logger.Config{
			SlowThreshold:             time.Second,
			LogLevel:                  config.LogLevel,
			IgnoreRecordNotFoundError: true,
			Colorful:                  true,
		},
	)

	// Open database connection
	db, err := gorm.Open(sqlite.Open(config.DSN), &gorm.Config{
		Logger: gormLogger,
		NowFunc: func() time.Time {
			return time.Now().UTC()
		},
	})
	if err != nil {
		return nil, fmt.Errorf("failed to connect to database: %w", err)
	}

	// Get underlying SQL DB for connection pool configuration
	sqlDB, err := db.DB()
	if err != nil {
		return nil, fmt.Errorf("failed to get underlying SQL DB: %w", err)
	}

	// Configure connection pool
	sqlDB.SetMaxOpenConns(config.MaxOpenConns)
	sqlDB.SetMaxIdleConns(config.MaxIdleConns)
	sqlDB.SetConnMaxLifetime(config.ConnMaxLifetime)

	// Test connection
	if err := sqlDB.Ping(); err != nil {
		return nil, fmt.Errorf("failed to ping database: %w", err)
	}

	// Run auto migrations
	if err := runMigrations(db); err != nil {
		return nil, fmt.Errorf("failed to run migrations: %w", err)
	}

	log.Printf("Database initialized successfully with DSN: %s", config.DSN)
	return db, nil
}

// runMigrations runs all database migrations
func runMigrations(db *gorm.DB) error {
	models := []interface{}{
		&models.AudioSession{},
		&models.TranscriptSegmentDB{},
		&models.SummaryDB{},
		&models.ProcessingJob{},
		&models.BatchJob{},
		&models.BatchJobFile{},
		&models.UserDB{},
	}

	for _, model := range models {
		if err := db.AutoMigrate(model); err != nil {
			return fmt.Errorf("failed to migrate %T: %w", model, err)
		}
	}

	// Create indexes for better performance
	if err := createIndexes(db); err != nil {
		return fmt.Errorf("failed to create indexes: %w", err)
	}

	log.Println("Database migrations completed successfully")
	return nil
}

// createIndexes creates additional database indexes for performance
func createIndexes(db *gorm.DB) error {
	// Index on audio_sessions for status and created_at
	if err := db.Exec("CREATE INDEX IF NOT EXISTS idx_audio_sessions_status ON audio_sessions(status)").Error; err != nil {
		return err
	}

	if err := db.Exec("CREATE INDEX IF NOT EXISTS idx_audio_sessions_created_at ON audio_sessions(created_at)").Error; err != nil {
		return err
	}

	// Index on transcript_segment_dbs for session_id and time range
	if err := db.Exec("CREATE INDEX IF NOT EXISTS idx_transcript_segments_session_time ON transcript_segment_dbs(session_id, start_time, end_time)").Error; err != nil {
		return err
	}

	// Index on processing_jobs for status and priority
	if err := db.Exec("CREATE INDEX IF NOT EXISTS idx_processing_jobs_status_priority ON processing_jobs(status, priority DESC)").Error; err != nil {
		return err
	}

	if err := db.Exec("CREATE INDEX IF NOT EXISTS idx_processing_jobs_created_at ON processing_jobs(created_at)").Error; err != nil {
		return err
	}

	return nil
}

// Close closes the database connection
func Close(db *gorm.DB) error {
	sqlDB, err := db.DB()
	if err != nil {
		return err
	}
	return sqlDB.Close()
}

// GetStats returns database connection pool statistics
func GetStats(db *gorm.DB) (interface{}, error) {
	sqlDB, err := db.DB()
	if err != nil {
		return nil, err
	}
	return sqlDB.Stats(), nil
}

// HealthCheck performs a basic database health check
func HealthCheck(db *gorm.DB) error {
	sqlDB, err := db.DB()
	if err != nil {
		return err
	}
	return sqlDB.Ping()
}
