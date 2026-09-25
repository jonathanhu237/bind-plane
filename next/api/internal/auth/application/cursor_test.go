package application

import (
	"testing"
)

func TestAccessCursorRoundTripPreservesQueryContext(t *testing.T) {
	want := AccessCursor{
		Version:   1,
		Query:     "Ada_%",
		RoleID:    "019535d9-3df7-79fb-b466-fa907fa17f95",
		Status:    "expired",
		Sort:      "createdAt",
		Direction: "desc",
		Value:     "2026-09-03T10:11:12.123456789Z",
		ID:        "019535d9-3df7-79fb-b466-fa907fa17f95",
	}
	encoded, err := EncodeAccessCursor(want)
	if err != nil {
		t.Fatalf("EncodeAccessCursor() error = %v", err)
	}
	got, err := DecodeAccessCursor(encoded)
	if err != nil {
		t.Fatalf("DecodeAccessCursor() error = %v", err)
	}
	if got != want {
		t.Fatalf("DecodeAccessCursor() = %#v, want %#v", got, want)
	}
}

func TestNormalizeAccessListOptionsRejectsCursorWithDifferentFilters(t *testing.T) {
	cursor, err := EncodeAccessCursor(AccessCursor{
		Version:   1,
		RoleID:    "019535d9-3df7-79fb-b466-fa907fa17f95",
		Status:    "pending",
		Sort:      "createdAt",
		Direction: "desc",
		Value:     "2026-09-03T10:11:12.123456789Z",
		ID:        "019535d9-3df7-79fb-b466-fa907fa17f96",
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := normalizeAccessListOptions(AccessListOptions{Cursor: cursor, RoleID: "019535d9-3df7-79fb-b466-fa907fa17f95", Status: "expired", Sort: "createdAt", Direction: "desc", Limit: 25}, true); err == nil {
		t.Fatal("normalizeAccessListOptions() accepted a cursor for different filters")
	}
}

func TestAccessCursorRejectsMalformedValues(t *testing.T) {
	for _, value := range []string{"", "not-base64", "eyJ2IjoyfQ"} {
		if _, err := DecodeAccessCursor(value); err != ErrInvalidCursor {
			t.Fatalf("DecodeAccessCursor(%q) error = %v, want ErrInvalidCursor", value, err)
		}
	}
}

func TestNormalizeAccessListOptionsNormalizesQueryAndValidatesCursorContext(t *testing.T) {
	options, err := normalizeAccessListOptions(AccessListOptions{Query: "  e\u0301  ", Limit: 0}, false)
	if err != nil {
		t.Fatalf("normalizeAccessListOptions() error = %v", err)
	}
	if options.Query != "é" || options.Sort != "createdAt" || options.Direction != "desc" || options.Limit != DefaultAccessPageSize {
		t.Fatalf("normalizeAccessListOptions() = %#v", options)
	}
	cursor, err := EncodeAccessCursor(AccessCursor{Version: 1, Query: "other", Sort: "createdAt", Direction: "desc", Value: "2026-09-03T10:11:12.123456789Z", ID: "019535d9-3df7-79fb-b466-fa907fa17f95"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := normalizeAccessListOptions(AccessListOptions{Cursor: cursor, Query: "é", Limit: 25}, false); err == nil {
		t.Fatal("normalizeAccessListOptions() accepted a cursor for a different query")
	}
}

func TestNormalizeAccessListOptionsAllowsRoleSortingOnlyForUsers(t *testing.T) {
	options, err := normalizeAccessListOptions(AccessListOptions{Sort: "roles", Direction: "asc", Limit: 25}, false)
	if err != nil {
		t.Fatalf("normalizeAccessListOptions(users) error = %v", err)
	}
	if options.Sort != "roles" || options.Direction != "asc" {
		t.Fatalf("normalizeAccessListOptions(users) = %#v, want role sorting", options)
	}
	if _, err := normalizeAccessListOptions(AccessListOptions{Sort: "roles", Direction: "asc", Limit: 25}, true); err == nil {
		t.Fatal("normalizeAccessListOptions(invitations) accepted role sorting")
	}
}

func TestNormalizeAccessListOptionsRejectsMalformedTimeCursor(t *testing.T) {
	cursor, err := EncodeAccessCursor(AccessCursor{Version: 1, Query: "", Sort: "createdAt", Direction: "desc", Value: "not-a-time", ID: "019535d9-3df7-79fb-b466-fa907fa17f95"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := normalizeAccessListOptions(AccessListOptions{Cursor: cursor, Sort: "createdAt", Direction: "desc", Limit: 25}, false); err == nil {
		t.Fatal("normalizeAccessListOptions() accepted a malformed time cursor")
	}
}
