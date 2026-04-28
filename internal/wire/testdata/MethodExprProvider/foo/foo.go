// Copyright 2018 The Wire Authors
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//     https://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

package main

import "fmt"

func main() {
	db, err := initDB()
	if err != nil {
		panic(err)
	}
	fmt.Println(db.DSN)
}

type Options struct {
	DSN string
}

type DB struct {
	DSN string
}

func provideOptions() *Options {
	return &Options{DSN: "postgres://wire"}
}

func (o *Options) ToDB() (*DB, error) {
	return &DB{DSN: o.DSN}, nil
}
