// Package regexutil provides safe regex compilation utilities
// to protect against ReDoS attacks via malicious regex patterns.
package regexutil

import (
	"log"
	"regexp"
	"time"
)

const (
	// MaxPatternLength is the maximum allowed length for a regex pattern in bytes.
	// Patterns exceeding this limit are rejected to prevent compilation-based DoS.
	MaxPatternLength = 1024

	// CompileTimeout is the maximum time allowed for regex compilation.
	CompileTimeout = 5 * time.Second
)

// SafeCompile compiles a regex pattern with safety limits.
// It rejects patterns longer than MaxPatternLength and aborts
// compilation that takes longer than CompileTimeout.
// Returns nil, error if the pattern is unsafe or fails to compile.
func SafeCompile(pattern string) (*regexp.Regexp, error) {
	// Reject excessively long patterns
	if len(pattern) > MaxPatternLength {
		return nil, &CompileError{
			Pattern: pattern,
			Reason:  "pattern exceeds maximum length",
		}
	}

	// Compile with a timeout using a goroutine
	type compileResult struct {
		re  *regexp.Regexp
		err error
	}

	done := make(chan compileResult, 1)
	go func() {
		re, err := regexp.Compile(pattern)
		done <- compileResult{re, err}
	}()

	select {
	case result := <-done:
		if result.err != nil {
			return nil, &CompileError{
				Pattern: pattern,
				Reason:  result.err.Error(),
			}
		}
		return result.re, nil
	case <-time.After(CompileTimeout):
		// Compilation timed out — the background goroutine is still
		// running regexp.Compile and will eventually write to done
		// (buffer=1). Start a drainer goroutine so it can exit
		// without blocking, then both can be garbage collected.
		go func() { <-done }()
		log.Printf("[Warning] Regex compilation timed out for pattern (first 100 chars): %s", truncate(pattern, 100))
		return nil, &CompileError{
			Pattern: pattern,
			Reason:  "compilation timed out",
		}
	}
}

// SafeCompileOrDefault compiles a regex pattern with safety limits.
// If compilation fails, it returns nil (no regex) and logs a warning.
// This is the preferred function for rules that can be safely skipped.
func SafeCompileOrDefault(pattern string) *regexp.Regexp {
	re, err := SafeCompile(pattern)
	if err != nil {
		log.Printf("[Warning] Skipping unsafe regex rule: %v", err)
		return nil
	}
	return re
}

// CompileError represents a regex compilation failure.
type CompileError struct {
	Pattern string
	Reason  string
}

func (e *CompileError) Error() string {
	return "regex compile error: " + e.Reason + " (pattern: " + truncate(e.Pattern, 100) + ")"
}

func truncate(s string, maxLen int) string {
	if len(s) <= maxLen {
		return s
	}
	return s[:maxLen] + "..."
}
