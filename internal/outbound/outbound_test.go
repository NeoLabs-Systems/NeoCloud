package outbound

import (
	"context"
	"errors"
	"net/netip"
	"slices"
	"strings"
	"testing"
)

func TestPolicies(t *testing.T) {
	private := []string{"127.0.0.1", "10.1.2.3", "172.20.0.1", "192.168.1.10", "100.64.0.1", "::1", "fd00::1", "::ffff:192.168.0.1", "0.0.0.0"}
	metadata := []string{"169.254.169.254", "100.100.100.200", "fd00:ec2::254", "fe80::1", "fe80::1%en0", "::ffff:169.254.169.254"}
	public := []string{"1.1.1.1", "93.184.216.34", "2606:4700::1111", "::ffff:8.8.8.8"}
	check := func(addresses []string, policy func(netip.Addr) error, refused bool) {
		for _, address := range addresses {
			if err := policy(netip.MustParseAddr(address)); errors.Is(err, ErrBlocked) != refused {
				t.Error(address, err)
			}
		}
	}
	check(private, PublicOnly, true)
	check(private, BlockMetadata, false)
	check(metadata, PublicOnly, true)
	check(metadata, BlockMetadata, true)
	check(public, PublicOnly, false)
}

func TestOnePrivateAddressRefusesTheName(t *testing.T) {
	guard := Guard{Check: PublicOnly, Resolve: func(context.Context, string) ([]netip.Addr, error) {
		return []netip.Addr{netip.MustParseAddr("203.0.114.5"), netip.MustParseAddr("10.0.0.7")}, nil
	}}
	if _, err := guard.Lookup(context.Background(), "cloud.example.com"); !errors.Is(err, ErrBlocked) {
		t.Fatal(err)
	}
	if _, err := guard.Lookup(context.Background(), "[::1]"); !errors.Is(err, ErrBlocked) {
		t.Fatal(err)
	}
}

func TestMultistatus(t *testing.T) {
	body := `<?xml version="1.0"?><x:multistatus xmlns:x="DAV:" xmlns:oc="http://owncloud.org/ns">
		<x:response><x:href> /a%20b/ </x:href>
		<x:propstat><x:prop><x:resourcetype><x:collection/></x:resourcetype><oc:size>7</oc:size></x:prop><x:status>HTTP/1.1 200 OK</x:status></x:propstat>
		<x:propstat><x:prop><x:getcontentlength>9</x:getcontentlength></x:prop><x:status>HTTP/1.1 404 Not Found</x:status></x:propstat>
		</x:response></x:multistatus>`
	responses, err := ReadMultistatus(strings.NewReader(body), 1<<20)
	if err != nil || len(responses) != 1 {
		t.Fatal(responses, err)
	}
	got := responses[0]
	if got.Href != "/a%20b/" || !got.IsCollection() || got.Text("size") != "7" || got.Text("getcontentlength") != "" {
		t.Fatalf("%+v", got)
	}
	if _, err = ReadMultistatus(strings.NewReader(body), 10); !errors.Is(err, ErrListingTooLarge) {
		t.Fatal(err)
	}
	for _, bad := range []string{"", "<html></html>", `<multistatus/>`, `<d:multistatus xmlns:d="DAV:">` + strings.Repeat("<d:response>", 90000)} {
		if _, err = ReadMultistatus(strings.NewReader(bad), 16<<20); !errors.Is(err, ErrNotWebDAV) {
			t.Fatal(err)
		}
	}
	rest, ok := Below(Segments("/Caf%C3%A9/"), Segments("/Cafe%CC%81/x%2Cy"))
	if !ok || !slices.Equal(rest, []string{"x,y"}) {
		t.Fatal(rest, ok)
	}
}
