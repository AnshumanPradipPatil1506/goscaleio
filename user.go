// Copyright © 2019 - 2022 Dell Inc. or its subsidiaries. All Rights Reserved.
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//      http://www.apache.org/licenses/LICENSE-2.0
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

package goscaleio

import (
	"fmt"
	"net/http"
	"time"

	types "github.com/AnshumanPradipPatil1506/goscaleio/types/v1"
)

// GetUser returns user
func (s *System) GetUser() ([]types.User, error) {
	defer TimeSpent("GetUser", time.Now())

	path := fmt.Sprintf("/api/instances/System::%v/relationships/User",
		s.System.ID)

	var user []types.User
	err := s.client.getJSONWithRetry(
		http.MethodGet, path, nil, &user)
	if err != nil {
		return nil, err
	}

	return user, nil
}
