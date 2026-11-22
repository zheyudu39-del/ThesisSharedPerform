package identity

import (
	"errors"
	"strings"
	"testing"

	"github.com/google/uuid"
)

// 乱码的来源：客户端把 GBK 字节按 UTF-8 解码，非法字节变成 U+FFFD，
// 而 U+FFFD 本身是合法字符，JSON 解码不会报错。值一旦落库就永久损坏
// ——原字节已经丢失，无法还原。必须在入口拒绝。
const replacement = "�"

// TestRegisterRejectsReplacementChars 注册时显示名含替换字符应被拒绝。
func TestRegisterRejectsReplacementChars(t *testing.T) {
	err := validateRegister(RegisterInput{
		Email:    "a@example.com",
		Password: "Passw0rd!2345",
		Name:     replacement + replacement,
		Handle:   "validhandle",
	})
	if !errors.Is(err, ValidationError) {
		t.Fatalf("应返回 ValidationError，实际 %v", err)
	}
	if !strings.Contains(err.Error(), "无法识别") {
		t.Fatalf("错误信息应说明字符无法识别，实际 %q", err.Error())
	}
}

// TestRegisterAcceptsNormalChinese 正常中文不应被误伤。
func TestRegisterAcceptsNormalChinese(t *testing.T) {
	err := validateRegister(RegisterInput{
		Email:    "a@example.com",
		Password: "Passw0rd!2345",
		Name:     "张伟·学者",
		Handle:   "validhandle",
	})
	if err != nil {
		t.Fatalf("正常中文被拒绝：%v", err)
	}
}

// TestUpdateProfileRejectsReplacementChars 改资料时同样要拦住。
func TestUpdateProfileRejectsReplacementChars(t *testing.T) {
	bad := replacement + "名字"
	_, err := buildUpdateProfileParams(uuid.Nil, UpdateProfileInput{Name: &bad})
	if !errors.Is(err, ValidationError) {
		t.Fatalf("显示名应被拒绝，实际 %v", err)
	}

	badBio := "简介" + replacement
	_, err = buildUpdateProfileParams(uuid.Nil, UpdateProfileInput{Bio: &badBio})
	if !errors.Is(err, ValidationError) {
		t.Fatalf("简介应被拒绝，实际 %v", err)
	}
}

// TestValidateTextAllowsEmpty 空值交给长度校验处理，编码校验不应先报错。
func TestValidateTextAllowsEmpty(t *testing.T) {
	if err := validateText("显示名", ""); err != nil {
		t.Fatalf("空串不应报编码错误：%v", err)
	}
}
