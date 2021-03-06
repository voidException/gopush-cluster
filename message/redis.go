// Copyright © 2014 Terry Mao, LiuDing All rights reserved.
// This file is part of gopush-cluster.

// gopush-cluster is free software: you can redistribute it and/or modify
// it under the terms of the GNU General Public License as published by
// the Free Software Foundation, either version 3 of the License, or
// (at your option) any later version.

// gopush-cluster is distributed in the hope that it will be useful,
// but WITHOUT ANY WARRANTY; without even the implied warranty of
// MERCHANTABILITY or FITNESS FOR A PARTICULAR PURPOSE.  See the
// GNU General Public License for more details.

// You should have received a copy of the GNU General Public License
// along with gopush-cluster.  If not, see <http://www.gnu.org/licenses/>.

package main

import (
	"encoding/json"
	"errors"
	"fmt"
	"github.com/Terry-Mao/gopush-cluster/hash"
	"github.com/garyburd/redigo/redis"
)

const (
	defaultRedisNode = "node1"
)

var (
	RedisNoConnErr = errors.New("can't get a redis conn")
)

// Initialize redis pool, Initialize consistency hash ring
func NewRedis() *RedisStorage {
	redisPool := map[string]*redis.Pool{}
	// Redis pool
	for n, addr := range Conf.RedisAddrs {
		tmp := addr
		// WARN: closures use
		redisPool[n] = &redis.Pool{
			MaxIdle:     Conf.RedisMaxIdle,
			MaxActive:   Conf.RedisMaxActive,
			IdleTimeout: Conf.RedisIdleTimeout,
			Dial: func() (redis.Conn, error) {
				conn, err := redis.Dial("tcp", tmp)
				if err != nil {
					Log.Error("redis.Dial(\"tcp\", \"%s\") error(%v)", tmp, err)
				}
				return conn, err
			},
		}
	}

	return &RedisStorage{Pool: redisPool, Ketama: hash.NewKetama(len(redisPool), 255)}
}

type RedisStorage struct {
	Pool   map[string]*redis.Pool
	Ketama *hash.Ketama
}

// Save offline messages
func (s *RedisStorage) Save(key string, msg *Message, mid int64) error {
	conn := s.getConn(key)
	if conn == nil {
		return RedisNoConnErr
	}
	defer conn.Close()

	message, _ := json.Marshal(*msg)

	//ZADD
	if err := conn.Send("ZADD", key, mid, string(message)); err != nil {
		return err
	}
	//ZREMRANGEBYRANK
	if err := conn.Send("ZREMRANGEBYRANK", key, 0, -1*(Conf.RedisMaxStore+1)); err != nil {
		return err
	}

	if err := conn.Flush(); err != nil {
		return err
	}

	//ZADD Receive
	_, err := conn.Receive()
	if err != nil {
		return err
	}
	//ZREMRANGEBYRANK Receive
	_, err = conn.Receive()
	if err != nil {
		return err
	}

	return nil
}

// Get all of offline messages which larger than mid
func (s *RedisStorage) Get(key string, mid int64) ([]string, error) {
	conn := s.getConn(key)
	if conn == nil {
		return nil, RedisNoConnErr
	}
	defer conn.Close()

	reply, err := redis.Strings(conn.Do("ZRANGEBYSCORE", key, fmt.Sprintf("(%d", mid), "+inf"))
	if err != nil {
		return nil, err
	}

	return reply, nil
}

// Delete multiple message
func (s *RedisStorage) DelMulti(info *DelMessageInfo) error {
	conn := s.getConn(info.Key)
	if conn == nil {
		return RedisNoConnErr
	}
	defer conn.Close()

	for _, msg := range info.Msgs {
		if err := conn.Send("ZREM", info.Key, msg); err != nil {
			return err
		}
	}

	if err := conn.Flush(); err != nil {
		return err
	}

	for _, _ = range info.Msgs {
		_, err := conn.Receive()
		if err != nil {
			return err
		}
	}

	return nil
}

// Delete key
func (s *RedisStorage) DelKey(key string) error {
	conn := s.getConn(key)
	if conn == nil {
		return RedisNoConnErr
	}
	defer conn.Close()

	_, err := conn.Do("DEL", key)
	if err != nil {
		return err
	}

	return nil
}

// Get the connection of matching with key
func (s *RedisStorage) getConn(key string) redis.Conn {
	node := defaultRedisNode
	if len(s.Pool) > 1 {
		node = s.Ketama.Node(key)
	}

	p, ok := s.Pool[node]
	if !ok {
		Log.Warn("no exists key:\"%s\" in redis pool", key)
		return nil
	}

	Log.Debug("key:\"%s\", hit node:\"%s\"", key, node)
	return p.Get()
}
