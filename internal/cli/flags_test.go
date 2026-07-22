package cli

import (
	"reflect"
	"testing"
)

func TestParseGlobalFlagsEqualsAndSpaceForms(t *testing.T) {
	tests := []struct {
		name string
		args []string
		want GlobalFlags
		rest []string
	}{
		{
			name: "space separated",
			args: []string{"--workspace", "/tmp/ws", "status"},
			want: GlobalFlags{Workspace: "/tmp/ws"},
			rest: []string{"status"},
		},
		{
			name: "equals form",
			args: []string{"--workspace=/tmp/ws", "status"},
			want: GlobalFlags{Workspace: "/tmp/ws"},
			rest: []string{"status"},
		},
		{
			name: "short flag -C",
			args: []string{"-C", "/tmp/ws", "status"},
			want: GlobalFlags{Workspace: "/tmp/ws"},
			rest: []string{"status"},
		},
		{
			name: "flags interleaved after command",
			args: []string{"status", "--json", "--actor=bob"},
			want: GlobalFlags{JSON: true, Actor: "bob"},
			rest: []string{"status"},
		},
		{
			name: "boolean flags",
			args: []string{"--json", "--silent", "status"},
			want: GlobalFlags{JSON: true, Silent: true},
			rest: []string{"status"},
		},
		{
			name: "removed db flag passes through unrecognized",
			args: []string{"--db", "/tmp/x.sqlite", "status"},
			want: GlobalFlags{},
			rest: []string{"--db", "/tmp/x.sqlite", "status"},
		},
		{
			name: "unrecognized flags pass through untouched",
			args: []string{"init", "--prefix", "foo", "path"},
			want: GlobalFlags{},
			rest: []string{"init", "--prefix", "foo", "path"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, rest, err := ParseGlobalFlags(tt.args)
			if err != nil {
				t.Fatalf("ParseGlobalFlags() error = %v", err)
			}
			if got != tt.want {
				t.Fatalf("ParseGlobalFlags() = %+v, want %+v", got, tt.want)
			}
			if !reflect.DeepEqual(rest, tt.rest) {
				t.Fatalf("rest = %v, want %v", rest, tt.rest)
			}
		})
	}
}

func TestParseGlobalFlagsMissingValue(t *testing.T) {
	_, _, err := ParseGlobalFlags([]string{"--actor"})
	if err == nil {
		t.Fatal("ParseGlobalFlags() error = nil, want error for missing value")
	}
}
