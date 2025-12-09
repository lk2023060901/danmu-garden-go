// Licensed to the LF AI & Data foundation under one
// or more contributor license agreements. See the NOTICE file
// distributed with this work for additional information
// regarding copyright ownership. The ASF licenses this file
// to you under the Apache License, Version 2.0 (the
// "License"); you may not use this file except in compliance
// with the License. You may obtain a copy of the License at
//
//     http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

package metrics

import (
	// #nosec
	_ "net/http/pprof"

	"github.com/prometheus/client_golang/prometheus"
)

const (
	// zeusNamespace 是当前项目所有 Prometheus 指标使用的命名空间。
	zeusNamespace = "zeus"

	// 以下为当前使用的通用标签名。
	nodeIDLabelName   = "node_id"
	roleNameLabelName = "role_name"

	lockName   = "lock_name"
	lockSource = "lock_source"
	lockType   = "lock_type"
	lockOp     = "lock_op"
)

var (
	// buckets 为请求耗时直方图的桶划分，单位为毫秒。
	// 实际桶分布为：
	// [1 2 4 8 16 32 64 128 256 512 1024 2048 4096 8192 16384 32768 65536 1.31072e+05]
	buckets = prometheus.ExponentialBuckets(1, 2, 18)

	// longTaskBuckets 为长耗时任务的桶划分，单位为毫秒。
	longTaskBuckets = []float64{1, 100, 500, 1000, 5000, 10000, 20000, 50000, 100000, 250000, 500000, 1000000, 3600000, 5000000, 10000000} // 单位：毫秒

	// sizeBuckets 为数据大小的桶划分，单位为字节。
	sizeBuckets = []float64{10000, 100000, 1000000, 100000000, 500000000, 1024000000, 2048000000, 4096000000, 10000000000, 50000000000} // 单位：字节

	NumNodes = prometheus.NewGaugeVec(
		prometheus.GaugeOpts{
			Namespace: zeusNamespace,
			Name:      "num_node",
			Help:      "number of nodes and coordinates",
		}, []string{nodeIDLabelName, roleNameLabelName})

	LockCosts = prometheus.NewGaugeVec(
		prometheus.GaugeOpts{
			Namespace: zeusNamespace,
			Name:      "lock_time_cost",
			Help:      "time cost for various kinds of locks",
		}, []string{
			lockName,
			lockSource,
			lockType,
			lockOp,
		})

	metricRegisterer prometheus.Registerer
)

// GetRegisterer 返回全局 Prometheus Registerer。
// 如果尚未通过 Register 显式设置，则返回 prometheus.DefaultRegisterer。
func GetRegisterer() prometheus.Registerer {
	if metricRegisterer == nil {
		return prometheus.DefaultRegisterer
	}
	return metricRegisterer
}

// Register 注册当前定义的所有指标。
// 通常应在 init 函数中调用。
func Register(r prometheus.Registerer) {
	r.MustRegister(NumNodes)
	r.MustRegister(LockCosts)
	metricRegisterer = r
}
