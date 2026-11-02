//go:build !windows

package ssh

import (
	"errors"
	"net"
	"os"

	gossh "golang.org/x/crypto/ssh"
	"golang.org/x/crypto/ssh/agent"
)

// dialAgent 连接到本机 SSH agent（Unix：SSH_AUTH_SOCK）
func dialAgent() (agent.ExtendedAgent, func(), error) {
	sock := os.Getenv("SSH_AUTH_SOCK")
	if sock == "" {
		return nil, nil, errors.New("SSH_AUTH_SOCK is not set (no ssh-agent running)")
	}
	conn, err := net.Dial("unix", sock)
	if err != nil {
		return nil, nil, err
	}
	return agent.NewClient(conn), func() { _ = conn.Close() }, nil
}

// agentAuthMethods 基于已连接的 agent 构造认证方法
func agentAuthMethods(ag agent.ExtendedAgent) []gossh.AuthMethod {
	return []gossh.AuthMethod{
		gossh.PublicKeysCallback(ag.Signers),
	}
}
