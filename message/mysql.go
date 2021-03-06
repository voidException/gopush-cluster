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
	"database/sql"
	"encoding/json"
	"errors"
	"github.com/Terry-Mao/gopush-cluster/hash"
	_ "github.com/go-sql-driver/mysql"
	"time"
)

const (
	defaultMYSQLNode = "node1"
	saveSQL          = "INSERT INTO message(sub,gid,mid,expire,msg,ctime,mtime) VALUES(?,?,?,?,?,?,?)"
	getSQL           = "SELECT msg FROM message WHERE key=? AND mid>? AND expire>?"
	delExpireSQL     = "DELETE FROM message WHERE expire<=?"
)

var (
	MYSQLNoDBErr = errors.New("can't get a mysql db")
)

// Initialize mysql pool, Initialize consistency hash ring
func NewMYSQL() *MYSQLStorage {
	dbPool := make(map[string]*sql.DB)
	for n, source := range Conf.DBSource {
		db, err := sql.Open("mysql", source)
		if err != nil {
			Log.Error("sql.Open(\"mysql\", %s) failed (%v)", source, err)
			panic(err)
		}

		dbPool[n] = db
	}

	return &MYSQLStorage{Pool: dbPool, Ketama: hash.NewKetama(len(dbPool), 255)}
}

type MYSQLStorage struct {
	Pool   map[string]*sql.DB
	Ketama *hash.Ketama
}

// Save offline messages
func (s *MYSQLStorage) Save(key string, msg *Message, mid int64) error {
	db := s.getConn(key)
	if db == nil {
		return MYSQLNoDBErr
	}

	message, _ := json.Marshal(*msg)
	now := time.Now()
	//TODO:change msg.Expire to second
	_, err := db.Exec(saveSQL, key, 0, mid, msg.Expire, string(message), now, now)
	if err != nil {
		Log.Error("db.Exec(%s,%s,%d,%d,%d,%s,now,now) failed (%v)", saveSQL, key, 0, mid, msg.Expire, string(message), now, now, err)
		return err
	}

	return nil
}

// Get all of offline messages which larger than mid
func (s *MYSQLStorage) Get(key string, mid int64) ([]string, error) {
	db := s.getConn(key)
	if db == nil {
		return nil, MYSQLNoDBErr
	}

	var msg []string
	now := time.Now()
	rows, err := db.Query(getSQL, key, mid, now)
	if err != nil {
		Log.Error("db.Query(%s,%s,%d,now) failed (%v)", getSQL, key, mid, err)
		return nil, err
	}

	for rows.Next() {
		var m string
		if err := rows.Scan(&m); err != nil {
			Log.Error("rows.Scan() failed (%v)", err)
			return nil, err
		}
		msg = append(msg, m)
	}

	return msg, nil
}

// Delete multiple messages
func (s *MYSQLStorage) DelMulti(info *DelMessageInfo) error {
	//TODO:nothing to do, cause delete operation run loop periodically
	return nil
}

// Delete key
func (s *MYSQLStorage) DelKey(key string) error {
	//TODO:nothing to do, cause delete operation run loop periodically
	return nil
}

// Delete all of expired messages
func (s *MYSQLStorage) DelAllExpired() error {
	now := time.Now()
	for _, db := range s.Pool {
		_, err := db.Exec(delExpireSQL, now)
		if err != nil {
			Log.Error("db.Exec(%s,now) failed (%v)", delExpireSQL, err)
			return err
		}
	}

	return nil
}

// Get the connection of matching with key
func (s *MYSQLStorage) getConn(key string) *sql.DB {
	node := defaultMYSQLNode
	if len(s.Pool) > 1 {
		node = s.Ketama.Node(key)
	}

	p, ok := s.Pool[node]
	if !ok {
		Log.Warn("no exists key:\"%s\" in redis pool", key)
		return nil
	}

	Log.Debug("key:\"%s\", hit node:\"%s\"", key, node)
	return p
}

// Loop delete expired messages
func (s *MYSQLStorage) delLoop() {
	for {
		if err := s.DelAllExpired(); err != nil {
			Log.Error("delete all of expired messages failed (%v)", err)
			time.Sleep(Conf.MYSQLDelLoopTime)
			continue
		}

		Log.Info("delete all of expired messages OK")
		time.Sleep(Conf.MYSQLDelLoopTime)
	}
}
