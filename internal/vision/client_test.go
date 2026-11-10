package vision

import "testing"

func TestLooksLikeCaptchaQuestion(t *testing.T) {
	if !looksLikeCaptchaQuestion("Recognize the CAPTCHA and output only the characters") {
		t.Fatal("expected captcha hint")
	}
	if looksLikeCaptchaQuestion("Describe the login-page layout") {
		t.Fatal("expected non-captcha")
	}
}
