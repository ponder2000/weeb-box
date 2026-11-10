package store

import (
	"context"
	"path/filepath"
	"testing"
)

func TestAnimeMeta(t *testing.T) {
	ctx := context.Background()
	st, err := Open(filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()

	if _, _, ok, err := st.GetAnimeMeta(ctx, "psyren"); err != nil || ok {
		t.Fatalf("missing ok = %v err = %v", ok, err)
	}

	if err := st.PutAnimeMeta(ctx, "psyren", `{"Title":"Psyren"}`); err != nil {
		t.Fatal(err)
	}
	payload, fetchedAt, ok, err := st.GetAnimeMeta(ctx, "psyren")
	if err != nil || !ok {
		t.Fatalf("ok = %v err = %v", ok, err)
	}
	if string(payload) != `{"Title":"Psyren"}` || fetchedAt.IsZero() {
		t.Fatalf("payload = %s fetched = %v", payload, fetchedAt)
	}

	if err := st.PutAnimeMeta(ctx, "psyren", `{"Title":"Psyren (updated)"}`); err != nil {
		t.Fatal(err)
	}
	payload, _, _, err = st.GetAnimeMeta(ctx, "psyren")
	if err != nil || string(payload) != `{"Title":"Psyren (updated)"}` {
		t.Fatalf("overwrite = %s err = %v", payload, err)
	}
}
