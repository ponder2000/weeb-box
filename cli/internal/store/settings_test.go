package store

import (
	"context"
	"path/filepath"
	"testing"
)

func TestSettings(t *testing.T) {
	ctx := context.Background()
	st, err := Open(filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()

	got, err := st.GetSetting(ctx, "theme", "carbon")
	if err != nil || got != "carbon" {
		t.Fatalf("fallback = %q err=%v", got, err)
	}

	if err := st.SetSetting(ctx, "theme", "dracula"); err != nil {
		t.Fatal(err)
	}
	if err := st.SetSetting(ctx, "theme", "nord"); err != nil {
		t.Fatal(err)
	}
	got, err = st.GetSetting(ctx, "theme", "carbon")
	if err != nil || got != "nord" {
		t.Fatalf("overwrite = %q err=%v", got, err)
	}

	if err := st.SetSetting(ctx, "empty", ""); err != nil {
		t.Fatal(err)
	}
	got, err = st.GetSetting(ctx, "empty", "fallback")
	if err != nil || got != "" {
		t.Fatalf("empty = %q err=%v", got, err)
	}
}
