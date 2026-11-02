package db

import (
	"context"
	"fmt"
	"strconv"
	"time"

	"github.com/redis/go-redis/v9"

	"github.com/lemon-puls/panoramix/internal/store"
)

// KeyInfo 键列表项
type KeyInfo struct {
	Key  string `json:"key"`
	TTL  int64  `json:"ttl"` // 秒；-1 无过期，-2 不存在
	Type string `json:"type"`
}

// KeyPage SCAN 一页结果
type KeyPage struct {
	Keys   []KeyInfo `json:"keys"`
	Cursor uint64    `json:"cursor"` // 0 表示遍历完成
}

// Value 键值详情（按类型填充对应字段）
type Value struct {
	Key       string            `json:"key"`
	Type      string            `json:"type"`
	TTL       int64             `json:"ttl"`
	Str       *string           `json:"str,omitempty"`
	Hash      map[string]string `json:"hash,omitempty"`
	List      []string          `json:"list,omitempty"`
	Set       []string          `json:"set,omitempty"`
	ZSet      []ZMember         `json:"zset,omitempty"`
	Total     int64             `json:"total"`     // 集合类元素总数
	Truncated bool              `json:"truncated"` // 大 value 截断预览
}

// ZMember 有序集合成员
type ZMember struct {
	Member string  `json:"member"`
	Score  float64 `json:"score"`
}

// redisPools Redis 客户端缓存（按连接 ID）
func (m *Manager) redisClient(c store.DBConn, password string) (*redis.Client, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if cli, ok := m.rdb[c.ID]; ok {
		return cli, nil
	}
	dbIdx := 0
	if n, err := strconv.Atoi(c.DBName); err == nil && n >= 0 {
		dbIdx = n
	}
	cli := redis.NewClient(&redis.Options{
		Addr:         c.Addr + ":" + strconv.Itoa(c.Port),
		Username:     c.Username,
		Password:     password,
		DB:           dbIdx,
		DialTimeout:  8 * time.Second,
		ReadTimeout:  10 * time.Second,
		WriteTimeout: 10 * time.Second,
	})
	ctx, cancel := context.WithTimeout(context.Background(), 8*time.Second)
	defer cancel()
	if err := cli.Ping(ctx).Err(); err != nil {
		_ = cli.Close()
		return nil, fmt.Errorf("ping redis: %w", err)
	}
	m.rdb[c.ID] = cli
	return cli, nil
}

// TestRedis 测试 Redis 连接（不缓存）
func TestRedis(c store.DBConn, password string) error {
	dbIdx := 0
	if n, err := strconv.Atoi(c.DBName); err == nil && n >= 0 {
		dbIdx = n
	}
	cli := redis.NewClient(&redis.Options{
		Addr:        c.Addr + ":" + strconv.Itoa(c.Port),
		Username:    c.Username,
		Password:    password,
		DB:          dbIdx,
		DialTimeout: 8 * time.Second,
	})
	defer cli.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 8*time.Second)
	defer cancel()
	return cli.Ping(ctx).Err()
}

// closeRedis 关闭并移除 Redis 客户端
func (m *Manager) closeRedis(id string) {
	m.mu.Lock()
	cli := m.rdb[id]
	delete(m.rdb, id)
	m.mu.Unlock()
	if cli != nil {
		_ = cli.Close()
	}
}

// RedisScan 渐进式扫描（绝不使用 KEYS *，见 ROADMAP v0.3）。
// cursor 为 0 表示从头开始；返回的 Cursor 为 0 表示遍历完成。
func (m *Manager) RedisScan(c store.DBConn, password string, cursor uint64, pattern string, count int) (*KeyPage, error) {
	cli, err := m.redisClient(c, password)
	if err != nil {
		return nil, err
	}
	if count <= 0 || count > 1000 {
		count = 200
	}
	if pattern == "" {
		pattern = "*"
	}
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()

	keys, next, err := cli.Scan(ctx, cursor, pattern, int64(count)).Result()
	if err != nil {
		return nil, fmt.Errorf("scan: %w", err)
	}
	page := &KeyPage{Cursor: next, Keys: make([]KeyInfo, 0, len(keys))}
	if len(keys) == 0 {
		return page, nil
	}
	// 批量取类型与 TTL（Pipeline 减少往返）
	pipe := cli.Pipeline()
	typeCmds := make([]*redis.StatusCmd, len(keys))
	ttlCmds := make([]*redis.DurationCmd, len(keys))
	for i, k := range keys {
		typeCmds[i] = pipe.Type(ctx, k)
		ttlCmds[i] = pipe.TTL(ctx, k)
	}
	_, _ = pipe.Exec(ctx)
	for i, k := range keys {
		ttl := int64(-1)
		if d := ttlCmds[i].Val(); d > 0 {
			ttl = int64(d.Seconds())
		} else if d < 0 {
			ttl = int64(d) // -1 无过期 / -2 不存在（go-redis 用 -1/-2 表示）
		}
		page.Keys = append(page.Keys, KeyInfo{Key: k, Type: typeCmds[i].Val(), TTL: ttl})
	}
	return page, nil
}

// redisPreviewLimit 集合类值的预览上限（大 value 截断，点击可全量加载）
const redisPreviewLimit = 200

// RedisValue 读取键值详情
func (m *Manager) RedisValue(c store.DBConn, password, key string) (*Value, error) {
	cli, err := m.redisClient(c, password)
	if err != nil {
		return nil, err
	}
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()

	typ, err := cli.Type(ctx, key).Result()
	if err != nil {
		return nil, fmt.Errorf("type: %w", err)
	}
	if typ == "none" {
		return nil, fmt.Errorf("key not found: %s", key)
	}
	v := &Value{Key: key, Type: typ}
	if ttl, terr := cli.TTL(ctx, key).Result(); terr == nil {
		if ttl > 0 {
			v.TTL = int64(ttl.Seconds())
		} else {
			v.TTL = int64(ttl) // -1 / -2
		}
	}
	switch typ {
	case "string":
		s, err := cli.Get(ctx, key).Result()
		if err != nil {
			return nil, fmt.Errorf("get: %w", err)
		}
		v.Str = &s
		v.Total = 1
	case "hash":
		all, err := cli.HGetAll(ctx, key).Result()
		if err != nil {
			return nil, fmt.Errorf("hgetall: %w", err)
		}
		v.Total = int64(len(all))
		v.Hash = map[string]string{}
		for k, val := range all {
			if len(v.Hash) >= redisPreviewLimit {
				v.Truncated = true
				break
			}
			v.Hash[k] = val
		}
	case "list":
		n, _ := cli.LLen(ctx, key).Result()
		v.Total = n
		limit := int64(redisPreviewLimit)
		if n > limit {
			v.Truncated = true
		}
		items, err := cli.LRange(ctx, key, 0, limit-1).Result()
		if err != nil {
			return nil, fmt.Errorf("lrange: %w", err)
		}
		v.List = items
	case "set":
		n, _ := cli.SCard(ctx, key).Result()
		v.Total = n
		items, _, err := cli.SScan(ctx, key, 0, "", int64(redisPreviewLimit)).Result()
		if err != nil {
			return nil, fmt.Errorf("sscan: %w", err)
		}
		if n > int64(len(items)) {
			v.Truncated = true
		}
		v.Set = items
	case "zset":
		n, _ := cli.ZCard(ctx, key).Result()
		v.Total = n
		items, err := cli.ZRangeWithScores(ctx, key, 0, int64(redisPreviewLimit)-1).Result()
		if err != nil {
			return nil, fmt.Errorf("zrange: %w", err)
		}
		if n > int64(len(items)) {
			v.Truncated = true
		}
		for _, it := range items {
			v.ZSet = append(v.ZSet, ZMember{Member: fmt.Sprint(it.Member), Score: it.Score})
		}
	default:
		// stream 等类型暂不支持查看（v0.3 范围外）
		return nil, fmt.Errorf("unsupported type: %s", typ)
	}
	return v, nil
}

// RedisSetTTL 设置键过期时间（秒；0 表示移除过期）
func (m *Manager) RedisSetTTL(c store.DBConn, password, key string, seconds int64) error {
	cli, err := m.redisClient(c, password)
	if err != nil {
		return err
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if seconds <= 0 {
		return cli.Persist(ctx, key).Err()
	}
	return cli.Expire(ctx, key, time.Duration(seconds)*time.Second).Err()
}

// RedisDelete 删除键
func (m *Manager) RedisDelete(c store.DBConn, password, key string) error {
	cli, err := m.redisClient(c, password)
	if err != nil {
		return err
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	return cli.Del(ctx, key).Err()
}

// RedisSetString 修改 string 类型值
func (m *Manager) RedisSetString(c store.DBConn, password, key, value string, keepTTL bool) error {
	cli, err := m.redisClient(c, password)
	if err != nil {
		return err
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	var ttl time.Duration
	if keepTTL {
		ttl = redis.KeepTTL
	}
	return cli.Set(ctx, key, value, ttl).Err()
}
