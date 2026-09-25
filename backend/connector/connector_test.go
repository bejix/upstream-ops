package connector

import (
	"errors"
	"testing"
)

func TestShouldLoginAfterRefreshErrorDefaultsToFallback(t *testing.T) {
	if !ShouldLoginAfterRefreshError(errors.New("legacy connector refresh failed")) {
		t.Fatal("untyped refresh error must preserve the existing login fallback")
	}
}

func TestShouldLoginAfterRefreshErrorHonorsPolicy(t *testing.T) {
	err := WrapSessionRefreshError(errors.New("temporary refresh failure"), false)
	if ShouldLoginAfterRefreshError(err) {
		t.Fatal("typed non-fallback refresh error allowed password login")
	}
}
