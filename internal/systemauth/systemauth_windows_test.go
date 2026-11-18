package systemauth

import (
	"errors"
	"os"
	"testing"
)

func TestUserConsentVerifierAvailability(t *testing.T) {
	if os.Getenv("GOIM_SYSTEM_AUTH_NATIVE") != "1" {
		t.Skip("set GOIM_SYSTEM_AUTH_NATIVE=1 to query Windows Hello")
	}
	availability, err := queryAvailability()
	if err != nil {
		t.Fatal(err)
	}
	if availability < userConsentVerifierAvailable || availability > userConsentDeviceBusy {
		t.Fatalf("availability = %d", availability)
	}
	t.Logf("Windows Hello availability = %d", availability)
}

func TestUserConsentVerifierInteropWhenUnavailable(t *testing.T) {
	if os.Getenv("GOIM_SYSTEM_AUTH_NATIVE") != "1" {
		t.Skip("set GOIM_SYSTEM_AUTH_NATIVE=1 to exercise the Windows Hello interop path")
	}
	availability, err := queryAvailability()
	if err != nil {
		t.Fatal(err)
	}
	if availability != userConsentDeviceNotPresent && availability != userConsentNotConfigured {
		t.Skipf("refusing to prompt while Windows Hello availability is %d", availability)
	}
	initialized, err := initializeWinRT()
	if err != nil {
		t.Fatal(err)
	}
	if initialized {
		defer roUninitialize.Call()
	}
	err = requestVerification(0, "GoIM system verification test")
	if errors.Is(err, ErrUnavailable) {
		t.Fatalf("interop path is unavailable: %v", err)
	}
	t.Logf("interop result = %v", err)
}
