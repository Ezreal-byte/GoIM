//go:build darwin && cgo

package systemauth

/*
#cgo CFLAGS: -x objective-c -fblocks
#cgo LDFLAGS: -framework Foundation -framework LocalAuthentication

#include <stdlib.h>

#import <Foundation/Foundation.h>
#import <LocalAuthentication/LocalAuthentication.h>

typedef struct {
	int verified;
	int code;
} goim_auth_result;

static goim_auth_result goim_verify_system_auth(const char *reason) {
	goim_auth_result result = {0, 0};
	@autoreleasepool {
		LAContext *context = [[LAContext alloc] init];
		NSError *error = nil;
		if (![context canEvaluatePolicy:LAPolicyDeviceOwnerAuthentication error:&error]) {
			result.code = error == nil ? 0 : (int)error.code;
			[context release];
			return result;
		}

		NSCondition *condition = [[NSCondition alloc] init];
		__block BOOL finished = NO;
		__block BOOL verified = NO;
		__block NSError *evaluationError = nil;
		NSString *localizedReason = reason == NULL
			? @"Unlock GoIM"
			: [NSString stringWithUTF8String:reason];

		[context evaluatePolicy:LAPolicyDeviceOwnerAuthentication
		        localizedReason:localizedReason
		                  reply:^(BOOL success, NSError * _Nullable authError) {
			[condition lock];
			verified = success;
			evaluationError = [authError retain];
			finished = YES;
			[condition signal];
			[condition unlock];
		}];
		[condition lock];
		while (!finished) {
			[condition wait];
		}
		[condition unlock];

		result.verified = verified ? 1 : 0;
		result.code = evaluationError == nil ? 0 : (int)evaluationError.code;
		[evaluationError release];
		[condition release];
		[context release];
	}
	return result;
}
*/
import "C"

import (
	"fmt"
	"unsafe"
)

const (
	laAuthenticationFailed = -1
	laUserCancel           = -2
	laUserFallback         = -3
	laSystemCancel         = -4
	laPasscodeNotSet       = -5
	laBiometryNotAvailable = -6
	laBiometryNotEnrolled  = -7
	laBiometryLockout      = -8
	laAppCancel            = -9
)

func description() string {
	return "Touch ID 或 macOS 登录密码"
}

func verify(reason string) error {
	if reason == "" {
		reason = "Unlock GoIM"
	}
	value := C.CString(reason)
	defer C.free(unsafe.Pointer(value))

	result := C.goim_verify_system_auth(value)
	if result.verified != 0 {
		return nil
	}
	switch int(result.code) {
	case laUserCancel, laSystemCancel, laAppCancel:
		return ErrCanceled
	case laPasscodeNotSet, laBiometryNotEnrolled:
		return ErrNotConfigured
	case laBiometryNotAvailable:
		return ErrUnavailable
	case laBiometryLockout:
		return ErrRetriesExhausted
	case laAuthenticationFailed, laUserFallback:
		return ErrFailed
	case 0:
		return fmt.Errorf("%w: LocalAuthentication returned no error code", ErrUnavailable)
	default:
		return fmt.Errorf("%w (LocalAuthentication error %d)", ErrFailed, int(result.code))
	}
}
