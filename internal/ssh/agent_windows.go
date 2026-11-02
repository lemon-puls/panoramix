//go:build windows

package ssh

import (
	"time"

	"github.com/Microsoft/go-winio"
	gossh "golang.org/x/crypto/ssh"
	"golang.org/x/crypto/ssh/agent"
)

// defaultAgentPipe Windows OpenSSH agent 的命名管道
const defaultAgentPipe = `\\.\pipe\openssh-ssh-agent`

// dialAgent 连接到 Windows OpenSSH agent（命名管道；Pageant 等第三方 agent
// 不提供该管道，连接失败按"agent 不可用"处理）
func dialAgent() (agent.ExtendedAgent, func(), error) {
	conn, err := winio.DialPipe(defaultAgentPipe, durationPtr(3 * time.Second))
	if err != nil {
		return nil, nil, err
	}
	return agent.NewClient(conn), func() { _ = conn.Close() }, nil
}

func durationPtr(d time.Duration) *time.Duration { return &d }

// agentAuthMethods 基于已连接的 agent 构造认证方法
func agentAuthMethods(ag agent.ExtendedAgent) []gossh.AuthMethod {
	return []gossh.AuthMethod{
		gossh.PublicKeysCallback(ag.Signers),
	}
}
