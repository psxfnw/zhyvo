package config

import "testing"

func TestInt64List(t *testing.T) {
	t.Setenv("TEST_ADMIN_IDS", "123, 456,123")
	result, err := int64List("TEST_ADMIN_IDS")
	if err != nil || len(result) != 2 || result[0] != 123 || result[1] != 456 {
		t.Fatalf("unexpected list: %#v, %v", result, err)
	}
	t.Setenv("TEST_ADMIN_IDS", "not-an-id")
	if _, err := int64List("TEST_ADMIN_IDS"); err == nil {
		t.Fatal("invalid Telegram ID was accepted")
	}
}
