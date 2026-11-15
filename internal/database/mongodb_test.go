package database

import (
	"testing"
	"time"
)

func TestNewClientOptionsConfiguresProductionResilience(t *testing.T) {
	uri := "mongodb://localhost:27017"
	opts := NewClientOptions(uri)

	if opts.ServerSelectionTimeout == nil || *opts.ServerSelectionTimeout != 5*time.Second {
		t.Errorf("expected ServerSelectionTimeout to be 5s, got %v", opts.ServerSelectionTimeout)
	}

	if opts.ConnectTimeout == nil || *opts.ConnectTimeout != 10*time.Second {
		t.Errorf("expected ConnectTimeout to be 10s, got %v", opts.ConnectTimeout)
	}

	if opts.Timeout == nil || *opts.Timeout != 15*time.Second {
		t.Errorf("expected Timeout (unified driver operation timeout) to be 15s, got %v", opts.Timeout)
	}

	if opts.MaxPoolSize == nil || *opts.MaxPoolSize != 50 {
		t.Errorf("expected MaxPoolSize to be 50, got %v", opts.MaxPoolSize)
	}

	if opts.MinPoolSize == nil || *opts.MinPoolSize != 5 {
		t.Errorf("expected MinPoolSize to be 5, got %v", opts.MinPoolSize)
	}

	if opts.MaxConnIdleTime == nil || *opts.MaxConnIdleTime != 5*time.Minute {
		t.Errorf("expected MaxConnIdleTime to be 5m, got %v", opts.MaxConnIdleTime)
	}
}

func TestConnectUnreachableHostFailsQuicklyWithoutHanging(t *testing.T) {
	start := time.Now()
	// An unreachable non-routable host: test must abort well within the 10-second timeout rather than hanging for 30-60s
	db, err := Connect("mongodb://127.0.0.1:1", "test_db")
	duration := time.Since(start)

	if err == nil {
		t.Fatalf("expected error connecting to unreachable host, got nil db: %v", db)
	}

	if duration > 10*time.Second {
		t.Fatalf("connection attempt took %v; expected failure within <= 10s timeout", duration)
	}
}
