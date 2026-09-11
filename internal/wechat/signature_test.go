package wechat

import "testing"

const (
	testToken     = "testtoken"
	testTimestamp = "1700000000"
	testNonce     = "abc123xyz"
	// 由 token/timestamp/nonce 字典序拼接后取 sha1 得到的已知值
	testSignature = "11d152d66ab57020da3bad11b845c1481e12b92b"
)

func TestCheckSignatureAcceptsValidSignature(t *testing.T) {
	if !CheckSignature(testToken, testSignature, testTimestamp, testNonce) {
		t.Fatal("合法签名应校验通过")
	}
}

func TestCheckSignatureRejectsInvalidInput(t *testing.T) {
	cases := map[string]struct {
		token     string
		signature string
	}{
		"签名不符":     {testToken, "deadbeef"},
		"token 不符": {"other-token", testSignature},
		"签名为空":     {testToken, ""},
		"token 为空": {"", testSignature},
	}

	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			if CheckSignature(tc.token, tc.signature, testTimestamp, testNonce) {
				t.Fatalf("非法输入不应通过校验: token=%q signature=%q", tc.token, tc.signature)
			}
		})
	}
}
