package metadata

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

// newTestClient routes "/anime" to the Jikan handler and everything else to AniList.
func newTestClient(t *testing.T, anilist, jikan http.HandlerFunc) *Client {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/anime" {
			jikan(w, r)
			return
		}
		anilist(w, r)
	}))
	t.Cleanup(srv.Close)
	return &Client{HTTP: srv.Client(), AniListURL: srv.URL, JikanURL: srv.URL, KitsuURL: ""}
}

func writeJSON(w http.ResponseWriter, body string) {
	w.Header().Set("Content-Type", "application/json")
	_, _ = w.Write([]byte(body))
}

const lookupMedia = `{"data":{"Media":{
	"title":{"romaji":"Psyren","english":"Psyren"},
	"coverImage":{"large":"https://img.example/psyren.jpg"},
	"genres":["Action","Mystery"],
	"tags":[
		{"name":"Time Manipulation","isMediaSpoiler":false},
		{"name":"Secret Identity","isMediaSpoiler":true},
		{"name":"T1","isMediaSpoiler":false},
		{"name":"T2","isMediaSpoiler":false},
		{"name":"T3","isMediaSpoiler":false},
		{"name":"T4","isMediaSpoiler":false},
		{"name":"T5","isMediaSpoiler":false},
		{"name":"T6","isMediaSpoiler":false},
		{"name":"T7","isMediaSpoiler":false},
		{"name":"T8","isMediaSpoiler":false},
		{"name":"T9","isMediaSpoiler":false}
	],
	"isAdult":false,
	"averageScore":72,
	"duration":24,
	"description":"A boy with psychic powers."
}}}`

func TestLookup(t *testing.T) {
	var query string
	var vars map[string]any
	client := newTestClient(t,
		func(w http.ResponseWriter, r *http.Request) {
			if r.Method != http.MethodPost {
				t.Errorf("anilist method = %s", r.Method)
			}
			var req struct {
				Query     string         `json:"query"`
				Variables map[string]any `json:"variables"`
			}
			if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
				t.Errorf("anilist body: %v", err)
			}
			query, vars = req.Query, req.Variables
			writeJSON(w, lookupMedia)
		},
		func(w http.ResponseWriter, r *http.Request) {
			if r.URL.Query().Get("q") != "Psyren" || r.URL.Query().Get("limit") != "1" {
				t.Errorf("jikan query = %s", r.URL.RawQuery)
			}
			writeJSON(w, `{"data":[{"rating":"PG-13 - Teens 13 or older"}]}`)
		})

	info, err := client.Lookup(context.Background(), "  Psyren ")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(query, "Media(search: $search, type: ANIME)") ||
		!strings.Contains(query, "description(asHtml: false)") ||
		!strings.Contains(query, "duration") {
		t.Fatalf("query = %s", query)
	}
	if vars["search"] != "Psyren" {
		t.Fatalf("variables = %+v", vars)
	}
	if info.Title != "Psyren" || info.CoverURL != "https://img.example/psyren.jpg" {
		t.Fatalf("info = %+v", info)
	}
	if len(info.Genres) != 2 || info.Genres[0] != "Action" {
		t.Fatalf("genres = %+v", info.Genres)
	}
	if len(info.Tags) != 8 || info.Tags[0] != "Time Manipulation" {
		t.Fatalf("tags = %+v", info.Tags)
	}
	for _, tag := range info.Tags {
		if tag == "Secret Identity" {
			t.Fatal("spoiler tag leaked")
		}
	}
	if info.Duration != 24 {
		t.Fatalf("duration = %d", info.Duration)
	}
	if info.Rating != "PG-13" || info.Adult {
		t.Fatalf("rating = %q adult = %v", info.Rating, info.Adult)
	}
	if info.Score != 72 {
		t.Fatalf("score = %v", info.Score)
	}
	if info.Description != "A boy with psychic powers." {
		t.Fatalf("description = %q", info.Description)
	}
}

func TestLookupKitsuRating(t *testing.T) {
	var jikanCalls int
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/anime":
			jikanCalls++
			writeJSON(w, `{"data":[]}`)
		case "/kitsu/anime":
			if r.URL.Query().Get("filter[text]") != "Frieren" {
				t.Errorf("kitsu query = %s", r.URL.RawQuery)
			}
			writeJSON(w, `{"data":[{"attributes":{"ageRating":"PG","ageRatingGuide":"Teens 13 or Older"}}]}`)
		default:
			writeJSON(w, lookupMedia)
		}
	}))
	defer srv.Close()
	client := &Client{HTTP: srv.Client(), AniListURL: srv.URL, JikanURL: srv.URL + "/jikan", KitsuURL: srv.URL + "/kitsu"}
	info, err := client.Lookup(context.Background(), "Frieren")
	if err != nil {
		t.Fatal(err)
	}
	if info.Duration != 24 {
		t.Fatalf("duration = %d", info.Duration)
	}
	if info.Rating != "PG-13" || info.Adult {
		t.Fatalf("rating = %q adult = %v", info.Rating, info.Adult)
	}
	if jikanCalls != 0 {
		t.Fatalf("jikan should not be called when Kitsu has a rating (%d)", jikanCalls)
	}
}

func TestLookupJikanRatingAndFallback(t *testing.T) {
	adultMedia := strings.Replace(lookupMedia, `"isAdult":false`, `"isAdult":true`, 1)
	anilist := func(w http.ResponseWriter, r *http.Request) { writeJSON(w, adultMedia) }

	cases := []struct {
		name   string
		status int
		body   string
		want   string
		adult  bool
	}{
		{"rating", http.StatusOK, `{"data":[{"rating":"R+ - Mild Nudity"}]}`, "R+", true},
		{"rx", http.StatusOK, `{"data":[{"rating":"Rx - Hentai"}]}`, "Rx", true},
		{"no match", http.StatusOK, `{"data":[]}`, "18+", true},
		{"jikan down", http.StatusInternalServerError, "boom", "18+", true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			client := newTestClient(t, anilist, func(w http.ResponseWriter, r *http.Request) {
				w.WriteHeader(tc.status)
				_, _ = w.Write([]byte(tc.body))
			})
			info, err := client.Lookup(context.Background(), "Psyren")
			if err != nil {
				t.Fatal(err)
			}
			if info.Rating != tc.want || info.Adult != tc.adult {
				t.Fatalf("rating = %q adult = %v", info.Rating, info.Adult)
			}
		})
	}

	t.Run("R is not adult", func(t *testing.T) {
		client := newTestClient(t,
			func(w http.ResponseWriter, r *http.Request) { writeJSON(w, lookupMedia) },
			func(w http.ResponseWriter, r *http.Request) {
				writeJSON(w, `{"data":[{"rating":"R - 17+ (violence & profanity)"}]}`)
			})
		info, err := client.Lookup(context.Background(), "Psyren")
		if err != nil {
			t.Fatal(err)
		}
		if info.Rating != "R" || info.Adult {
			t.Fatalf("rating = %q adult = %v", info.Rating, info.Adult)
		}
	})

	t.Run("not adult fallback", func(t *testing.T) {
		client := newTestClient(t,
			func(w http.ResponseWriter, r *http.Request) { writeJSON(w, lookupMedia) },
			func(w http.ResponseWriter, r *http.Request) {
				w.WriteHeader(http.StatusBadGateway)
			})
		info, err := client.Lookup(context.Background(), "Psyren")
		if err != nil {
			t.Fatal(err)
		}
		if info.Rating != "Not rated" || info.Adult {
			t.Fatalf("rating = %q adult = %v", info.Rating, info.Adult)
		}
	})
}

func TestBrowseAdultFilter(t *testing.T) {
	var query string
	client := newTestClient(t,
		func(w http.ResponseWriter, r *http.Request) {
			var req struct {
				Query string `json:"query"`
			}
			if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
				t.Errorf("body: %v", err)
			}
			query = req.Query
			writeJSON(w, `{"data":{"Page":{"media":[]}}}`)
		},
		func(w http.ResponseWriter, r *http.Request) {
			t.Error("browse should not call jikan")
		})
	if _, err := client.Browse(context.Background(), "Action", 1, true); err != nil {
		t.Fatal(err)
	}
	if strings.Contains(query, "isAdult:") {
		t.Fatalf("adult browse kept the isAdult filter: %s", query)
	}
	if !strings.Contains(query, "genre: $genre") {
		t.Fatalf("query missing genre variable: %s", query)
	}
}

func TestBrowse(t *testing.T) {
	var query string
	var vars map[string]any
	jikanCalls := 0
	client := newTestClient(t,
		func(w http.ResponseWriter, r *http.Request) {
			var req struct {
				Query     string         `json:"query"`
				Variables map[string]any `json:"variables"`
			}
			if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
				t.Errorf("body: %v", err)
			}
			query, vars = req.Query, req.Variables
			writeJSON(w, `{"data":{"Page":{"media":[
				{"title":{"romaji":"One","english":""},"coverImage":{"large":"https://x/1.jpg"},
				 "genres":["Adventure"],"tags":[],"isAdult":false,"averageScore":80,"description":"d1"},
				{"title":{"romaji":"Two","english":"Two"},"coverImage":{"large":"https://x/2.jpg"},
				 "genres":["Adventure"],"tags":[],"isAdult":true,"averageScore":70,"description":"d2"}
			]}}}`)
		},
		func(w http.ResponseWriter, r *http.Request) {
			jikanCalls++
			writeJSON(w, `{"data":[{"rating":"PG-13 - Teens 13 or older"}]}`)
		})

	items, err := client.Browse(context.Background(), "Adventure", 2, false)
	if err != nil {
		t.Fatal(err)
	}
	if jikanCalls != 0 {
		t.Fatalf("browse called jikan %d times", jikanCalls)
	}
	for _, part := range []string{"Page(page: $page, perPage: 20)", "genre: $genre", "isAdult: false", "sort: POPULARITY_DESC"} {
		if !strings.Contains(query, part) {
			t.Fatalf("query missing %q: %s", part, query)
		}
	}
	if vars["genre"] != "Adventure" || vars["page"] != float64(2) {
		t.Fatalf("variables = %+v", vars)
	}
	if len(items) != 2 {
		t.Fatalf("items = %+v", items)
	}
	if items[0].Title != "One" || items[0].Rating != "Not rated" || items[0].Adult {
		t.Fatalf("first = %+v", items[0])
	}
	if items[1].Rating != "18+" || !items[1].Adult {
		t.Fatalf("second = %+v", items[1])
	}
}

func TestSeasonFor(t *testing.T) {
	cases := []struct {
		name   string
		when   time.Time
		season string
		year   int
	}{
		{"winter start", time.Date(2024, time.January, 1, 0, 0, 0, 0, time.UTC), "WINTER", 2024},
		{"winter end", time.Date(2024, time.March, 31, 23, 59, 0, 0, time.UTC), "WINTER", 2024},
		{"spring", time.Date(2024, time.April, 1, 0, 0, 0, 0, time.UTC), "SPRING", 2024},
		{"summer", time.Date(2024, time.July, 1, 0, 0, 0, 0, time.UTC), "SUMMER", 2024},
		{"fall", time.Date(2024, time.October, 1, 0, 0, 0, 0, time.UTC), "FALL", 2024},
		{"fall end", time.Date(2024, time.December, 31, 23, 59, 0, 0, time.UTC), "FALL", 2024},
		// UTC-5 New Year's Eve is already January in UTC, so the year rolls too.
		{"local year boundary", time.Date(2024, time.December, 31, 23, 30, 0, 0, time.FixedZone("UTC-5", -5*3600)), "WINTER", 2025},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			season, year := SeasonFor(tc.when)
			if season != tc.season || year != tc.year {
				t.Fatalf("SeasonFor(%s) = %s %d, want %s %d", tc.when, season, year, tc.season, tc.year)
			}
		})
	}
}

func TestSeasonSteps(t *testing.T) {
	if season, year := NextSeason("FALL", 2024); season != "WINTER" || year != 2025 {
		t.Fatalf("NextSeason(FALL, 2024) = %s %d", season, year)
	}
	if season, year := PrevSeason("WINTER", 2024); season != "FALL" || year != 2023 {
		t.Fatalf("PrevSeason(WINTER, 2024) = %s %d", season, year)
	}
	for _, season := range seasons {
		for _, year := range []int{2020, 2024} {
			next, nextYear := NextSeason(season, year)
			if gotSeason, gotYear := PrevSeason(next, nextYear); gotSeason != season || gotYear != year {
				t.Fatalf("round trip of %s %d = %s %d", season, year, gotSeason, gotYear)
			}
		}
	}
}

func TestBrowseSeason(t *testing.T) {
	media := `{"data":{"Page":{"media":[
		{"title":{"romaji":"One","english":""},"coverImage":{"large":"https://x/1.jpg"},
		 "genres":["Adventure"],"tags":[],"isAdult":false,"averageScore":80,"description":"d1"},
		{"title":{"romaji":"Two","english":"Two"},"coverImage":{"large":"https://x/2.jpg"},
		 "genres":["Adventure"],"tags":[],"isAdult":true,"averageScore":70,"description":"d2"}
	]}}}`

	for _, tc := range []struct {
		name   string
		adult  bool
		filter bool
	}{
		{"non-adult", false, true},
		{"adult", true, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var query string
			var vars map[string]any
			client := newTestClient(t,
				func(w http.ResponseWriter, r *http.Request) {
					var req struct {
						Query     string         `json:"query"`
						Variables map[string]any `json:"variables"`
					}
					if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
						t.Errorf("body: %v", err)
					}
					query, vars = req.Query, req.Variables
					writeJSON(w, media)
				},
				func(w http.ResponseWriter, r *http.Request) {
					t.Error("browse should not call jikan")
				})

			items, err := client.BrowseSeason(context.Background(), " winter ", 2024, 3, tc.adult)
			if err != nil {
				t.Fatal(err)
			}
			for _, part := range []string{
				"Page(page: $page, perPage: 20)",
				"season: $season",
				"seasonYear: $seasonYear",
				"type: ANIME",
				"sort: POPULARITY_DESC",
			} {
				if !strings.Contains(query, part) {
					t.Fatalf("query missing %q: %s", part, query)
				}
			}
			if got := strings.Contains(query, "isAdult: false"); got != tc.filter {
				t.Fatalf("isAdult filter = %v, want %v: %s", got, tc.filter, query)
			}
			if _, ok := vars["isAdult"]; ok {
				t.Fatalf("isAdult must not be a variable: %+v", vars)
			}
			if vars["season"] != "WINTER" || vars["seasonYear"] != float64(2024) || vars["page"] != float64(3) {
				t.Fatalf("variables = %+v", vars)
			}
			if len(items) != 2 {
				t.Fatalf("items = %+v", items)
			}
			if items[0].Title != "One" || items[0].Rating != "Not rated" || items[0].Adult {
				t.Fatalf("first = %+v", items[0])
			}
			if items[1].Rating != "18+" || !items[1].Adult {
				t.Fatalf("second = %+v", items[1])
			}
		})
	}
}

func TestBrowseSeasonError(t *testing.T) {
	client := newTestClient(t,
		func(w http.ResponseWriter, r *http.Request) {
			writeJSON(w, `{"errors":[{"message":"Invalid season"}]}`)
		},
		func(w http.ResponseWriter, r *http.Request) { writeJSON(w, `{"data":[]}`) })

	if _, err := client.BrowseSeason(context.Background(), "WINTER", 2024, 1, false); err == nil ||
		!strings.Contains(err.Error(), "Invalid season") {
		t.Fatalf("err = %v", err)
	}
}

func TestLookupErrors(t *testing.T) {
	client := newTestClient(t,
		func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(http.StatusInternalServerError)
			_, _ = w.Write([]byte("upstream exploded"))
		},
		func(w http.ResponseWriter, r *http.Request) { writeJSON(w, `{"data":[]}`) })

	if _, err := client.Lookup(context.Background(), "Psyren"); err == nil ||
		!strings.Contains(err.Error(), "500") {
		t.Fatalf("err = %v", err)
	}
	if _, err := client.Lookup(context.Background(), "   "); err == nil {
		t.Fatal("empty title should error")
	}
}

func TestKey(t *testing.T) {
	if got := Key("  One Punch Man "); got != "one punch man" {
		t.Fatalf("Key = %q", got)
	}
}
