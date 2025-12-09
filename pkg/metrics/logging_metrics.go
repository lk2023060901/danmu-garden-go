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
	"sync"

	"github.com/prometheus/client_golang/prometheus"
)

const (
	loggingMetricSubsystem = "logging"
)

var (
	LoggingMetricsRegisterOnce sync.Once

	LoggingPendingWriteLength = prometheus.NewGauge(prometheus.GaugeOpts{
		Namespace: zeusNamespace,
		Subsystem: loggingMetricSubsystem,
		Name:      "pending_write_length",
		Help:      "当前日志缓冲区中待写入日志的数量",
	})

	LoggingPendingWriteBytes = prometheus.NewGauge(prometheus.GaugeOpts{
		Namespace: zeusNamespace,
		Subsystem: loggingMetricSubsystem,
		Name:      "pending_write_bytes",
		Help:      "当前日志缓冲区中待写入日志的总字节数",
	})

	LoggingTruncatedWrites = prometheus.NewGauge(prometheus.GaugeOpts{
		Namespace: zeusNamespace,
		Subsystem: loggingMetricSubsystem,
		Name:      "truncated_writes",
		Help:      "单条日志超过最大字节数而被截断的次数",
	})

	LoggingTruncatedWriteBytes = prometheus.NewGauge(prometheus.GaugeOpts{
		Namespace: zeusNamespace,
		Subsystem: loggingMetricSubsystem,
		Name:      "truncated_write_bytes",
		Help:      "因单条日志超过最大字节数而被截断的总字节数",
	})

	LoggingDroppedWrites = prometheus.NewGauge(prometheus.GaugeOpts{
		Namespace: zeusNamespace,
		Subsystem: loggingMetricSubsystem,
		Name:      "dropped_writes",
		Help:      "由于缓冲区已满或写入超时而被丢弃的日志条数",
	})

	LoggingIOFailure = prometheus.NewGauge(prometheus.GaugeOpts{
		Namespace: zeusNamespace,
		Subsystem: loggingMetricSubsystem,
		Name:      "io_failures",
		Help:      "由于底层写入阻塞或写入超时导致的 IO 失败次数",
	})
)

// RegisterLoggingMetrics 将日志相关的指标注册到 Prometheus Registry 中。
func RegisterLoggingMetrics(registry *prometheus.Registry) {
	LoggingMetricsRegisterOnce.Do(func() {
		registry.MustRegister(LoggingPendingWriteLength)
		registry.MustRegister(LoggingPendingWriteBytes)
		registry.MustRegister(LoggingTruncatedWrites)
		registry.MustRegister(LoggingTruncatedWriteBytes)
		registry.MustRegister(LoggingDroppedWrites)
		registry.MustRegister(LoggingIOFailure)
	})
}
