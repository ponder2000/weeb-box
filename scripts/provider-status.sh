#!/bin/sh
# Runs every provider's live test and rewrites the README status table.
# Exits 1 when a provider fails, after the table is written, so CI still records it.
set -u

cd "$(dirname "$0")/.."

table=$(mktemp)
{
	printf '| Provider | Live check | Checked (UTC) |\n'
	printf '| --- | --- | --- |\n'
} >"$table"

failed=""
for dir in internal/provider/*/; do
	pkg=${dir#internal/provider/}
	pkg=${pkg%/}
	# only adapters with a live test
	grep -qs 'func TestLive' "$dir"*_test.go || continue
	name=$pkg
	[ "$pkg" = gogo ] && name=gogoanime
	if out=$(WEEB_LIVE=1 go test "./internal/provider/$pkg" -run Live -count=1 2>&1); then
		printf '| `%s` | ✅ working | %s |\n' "$name" "$(date -u +%Y-%m-%d)" >>"$table"
	else
		printf '| `%s` | ❌ failing | %s |\n' "$name" "$(date -u +%Y-%m-%d)" >>"$table"
		failed="$failed $name"
		printf '%s\n' "$out" | tail -20 >&2
	fi
done

awk -v table="$table" '
	/<!-- provider-status:start -->/ { print; while ((getline line < table) > 0) print line; skip = 1; next }
	/<!-- provider-status:end -->/ { skip = 0 }
	!skip' README.md >"$table.readme"
mv "$table.readme" README.md
rm -f "$table"

if [ -n "$failed" ]; then
	echo "live check failed:$failed" >&2
	exit 1
fi
