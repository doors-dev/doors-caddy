package geo

import (
	"slices"
	"testing"

	"github.com/caddyserver/caddy/v2/caddyconfig/caddyfile"
)

func TestUnmarshalCaddyfile(t *testing.T) {
	tests := []struct {
		name        string
		input       string
		expect      map[string][]string
		expectError bool
	}{
		{
			name: "single domain, single line of country codes",
			input: `doors_geo {
	doors.dev {
		US CA MX
	}
}`,
			expect: map[string][]string{
				"doors.dev": {"US", "CA", "MX"},
			},
		},
		{
			name: "single domain, multi-line country codes",
			input: `doors_geo {
	doors.dev {
		AG AI AR AW BB BL BM BO BQ BR
		BS BZ CA CK CL CO CR CU CW DM
		DO EC FK GD GF GL GP GT GY HN
		HT JM KN KY LC MF MH MQ MS MX
		NI NU PA PE PF PM PR PY SR SV
		SX TC TO TT US UY VC VE VG VI
		WS
	}
}`,
			expect: map[string][]string{
				"doors.dev": {
					"AG", "AI", "AR", "AW", "BB", "BL", "BM", "BO", "BQ", "BR",
					"BS", "BZ", "CA", "CK", "CL", "CO", "CR", "CU", "CW", "DM",
					"DO", "EC", "FK", "GD", "GF", "GL", "GP", "GT", "GY", "HN",
					"HT", "JM", "KN", "KY", "LC", "MF", "MH", "MQ", "MS", "MX",
					"NI", "NU", "PA", "PE", "PF", "PM", "PR", "PY", "SR", "SV",
					"SX", "TC", "TO", "TT", "US", "UY", "VC", "VE", "VG", "VI",
					"WS",
				},
			},
		},
		{
			name: "multiple domains with multi-line country codes",
			input: `doors_geo {
	doors.dev {
		US CA
		MX
	}
	eu.doors.dev {
		DE FR IT
		ES PT NL
	}
	e.doors.dev {
		JP KR
		CN
	}
}`,
			expect: map[string][]string{
				"doors.dev":    {"US", "CA", "MX"},
				"eu.doors.dev": {"DE", "FR", "IT", "ES", "PT", "NL"},
				"e.doors.dev":  {"JP", "KR", "CN"},
			},
		},
		{
			name: "domain with no country codes",
			input: `doors_geo {
	doors.dev {
	}
}`,
			expectError: true,
		},
		{
			name: "invalid country code length",
			input: `doors_geo {
	doors.dev {
		USA
	}
}`,
			expectError: true,
		},
		{
			name: "with config options",
			input: `doors_geo {
	ipv4_url https://example.com/ipv4.tar.gz
	update_interval 12h
	doors.dev {
		US CA
	}
}`,
			expect: map[string][]string{
				"doors.dev": {"US", "CA"},
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			dispenser := caddyfile.NewTestDispenser(tt.input)
			var m Module
			err := m.UnmarshalCaddyfile(dispenser)
			if tt.expectError {
				if err == nil {
					t.Fatal("expected error but got nil")
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if len(m.Redirects) != len(tt.expect) {
				t.Fatalf("expected %d domains, got %d: %v", len(tt.expect), len(m.Redirects), m.Redirects)
			}
			for domain, expectedCodes := range tt.expect {
				gotCodes, ok := m.Redirects[domain]
				if !ok {
					t.Fatalf("expected domain %q not found in result: %v", domain, m.Redirects)
				}
				if !slices.Equal(gotCodes, expectedCodes) {
					t.Fatalf("domain %q: expected %v, got %v", domain, expectedCodes, gotCodes)
				}
			}
		})
	}
}
