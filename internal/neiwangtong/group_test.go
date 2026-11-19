package neiwangtong

import (
	"strings"
	"testing"
)

func TestGroupRosterCarriesNativePhotoDigest(t *testing.T) {
	id := strings.Repeat("a", 32)
	owner := strings.Repeat("b", 32)
	digest := strings.Repeat("c", 32)
	raw := []byte(`<X_QGROUP_PUSH_USER docver="1"><INFO>` +
		`<QGROUP_ID>` + id + `</QGROUP_ID><QGROUP_NAME>设计群</QGROUP_NAME>` +
		`<QGROUP_MASTER>` + owner + `</QGROUP_MASTER><QGROUP_INFO_VER>2</QGROUP_INFO_VER>` +
		`<QGROUP_USER_VER>2</QGROUP_USER_VER><QGROUP_PHOTO_MD5>` + digest +
		`</QGROUP_PHOTO_MD5></INFO><USER><ID name="群主">` + owner + `</ID></USER></X_QGROUP_PUSH_USER>`)
	roster, err := ParseGroupRoster(raw)
	if err != nil {
		t.Fatal(err)
	}
	if roster.PhotoMD5 != digest {
		t.Fatalf("photo digest = %q, want %q", roster.PhotoMD5, digest)
	}

	roster.PhotoMD5 = "not-a-digest"
	if err := roster.validate(); err == nil {
		t.Fatal("invalid native group photo digest was accepted")
	}
}
