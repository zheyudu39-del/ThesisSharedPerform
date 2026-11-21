package file

import (
	"testing"

	"github.com/thesis-shared/api/internal/db"
)

// TestMIMEKindLaTeX 验证 LaTeX 源文件能被识别。
//
// 浏览器对 .tex 的 File.type 取值不统一（空串 / text/x-tex /
// application/x-tex），前端按扩展名兜底成 text/x-tex 后仍要能被服务端接住。
func TestMIMEKindLaTeX(t *testing.T) {
	for _, mime := range []string{"text/x-tex", "application/x-tex", "application/x-latex", "TEXT/X-TEX; charset=utf-8"} {
		kind, ok := MIMEKind(mime)
		if !ok {
			t.Fatalf("MIMEKind(%q) 应被接受", mime)
		}
		if kind != db.FileKindLatex {
			t.Fatalf("MIMEKind(%q) = %q, want %q", mime, kind, db.FileKindLatex)
		}
	}
}

// TestExtForLaTeX 验证存储 key 用 .tex 后缀，下载时文件名才不会丢。
func TestExtForLaTeX(t *testing.T) {
	if got := extFor("text/x-tex"); got != ".tex" {
		t.Fatalf("extFor(text/x-tex) = %q, want .tex", got)
	}
}

// TestMIMEKindRejectsExecutable 确认白名单没有因为加 LaTeX 而放宽。
func TestMIMEKindRejectsExecutable(t *testing.T) {
	for _, mime := range []string{"application/x-msdownload", "application/zip", "text/x-sh"} {
		if _, ok := MIMEKind(mime); ok {
			t.Fatalf("MIMEKind(%q) 不应被接受", mime)
		}
	}
}
