package wsclient

import "sync"

// Enrollee 提供 Agent 的注册资料与凭证读写。
// 由上层注入（从配置读取 server url / token，从磁盘读写凭证），便于测试替换。
type Enrollee interface {
	// ServerURL 返回 CM 的反连地址（ws(s)://host:port/agent/ws）。
	ServerURL() string
	// EnrollmentToken 返回首注册令牌（已注册后可返回空）。
	EnrollmentToken() string
	// NodeName 返回上报的节点名。
	NodeName() string
	// Credential 返回本地已保存凭证；未注册返回 (nil, nil)。
	Credential() (*Credential, error)
	// Save 持久化新下发的凭证。
	Save(*Credential) error
}

// StaticEnrollee 是基于内存/静态值的 Enrollee 实现。
type StaticEnrollee struct {
	URL   string
	Token string
	Name  string

	mu    sync.Mutex
	cred  *Credential
	saver func(*Credential) error
}

// NewStaticEnrollee 构造静态注册资料。saver 为 nil 时凭证仅存内存。
func NewStaticEnrollee(url, token, name string, cred *Credential, saver func(*Credential) error) *StaticEnrollee {
	return &StaticEnrollee{URL: url, Token: token, Name: name, cred: cred, saver: saver}
}

// ServerURL 实现 Enrollee。
func (e *StaticEnrollee) ServerURL() string { return e.URL }

// EnrollmentToken 实现 Enrollee。
func (e *StaticEnrollee) EnrollmentToken() string { return e.Token }

// NodeName 实现 Enrollee。
func (e *StaticEnrollee) NodeName() string { return e.Name }

// Credential 实现 Enrollee。
func (e *StaticEnrollee) Credential() (*Credential, error) {
	e.mu.Lock()
	defer e.mu.Unlock()
	return e.cred, nil
}

// Save 实现 Enrollee。
func (e *StaticEnrollee) Save(c *Credential) error {
	e.mu.Lock()
	e.cred = c
	saver := e.saver
	e.mu.Unlock()
	if saver != nil {
		return saver(c)
	}
	return nil
}
