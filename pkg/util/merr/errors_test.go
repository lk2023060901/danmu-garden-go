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

package merr

import (
	"context"
	"os"
	"testing"

	"github.com/cockroachdb/errors"
	"github.com/stretchr/testify/suite"

	commonpb "github.com/lk2023060901/danmu-garden-framework-protos/framework"
)

type ErrSuite struct {
	suite.Suite
}

func (s *ErrSuite) TestCode() {
	err := WrapErrCollectionNotFound(1)
	errors.Wrap(err, "failed to get collection")
	s.ErrorIs(err, ErrCollectionNotFound)
	s.Equal(Code(ErrCollectionNotFound), Code(err))
	s.Equal(TimeoutCode, Code(context.DeadlineExceeded))
	s.Equal(CanceledCode, Code(context.Canceled))
	s.Equal(errUnexpected.errCode, Code(errUnexpected))

	sameCodeErr := newZeusError("new error", ErrCollectionNotFound.errCode, false)
	s.True(sameCodeErr.Is(ErrCollectionNotFound))
}

func (s *ErrSuite) TestStatus() {
	err := WrapErrCollectionNotFound(1)
	status := Status(err)
	restoredErr := Error(status)

	s.ErrorIs(err, restoredErr)
	s.Equal(int32(0), Status(nil).Code)
	s.Nil(Error(&commonpb.Status{}))
}

func (s *ErrSuite) TestStatusWithCode() {
	err := WrapErrCollectionNotFound(1)
	const code int32 = 100
	status := StatusWithErrorCode(err, code)
	restoredErr := Error(status)

	s.ErrorIs(err, restoredErr)
	s.Equal(code, status.Code)
	s.Equal(int32(0), StatusWithErrorCode(nil, code).Code)
}

func (s *ErrSuite) TestWrap() {
	// Service 相关错误。
	s.ErrorIs(WrapErrServiceNotReady("test", 0, "test init..."), ErrServiceNotReady)
	s.ErrorIs(WrapErrServiceUnavailable("test", "test init"), ErrServiceUnavailable)
	s.ErrorIs(WrapErrServiceMemoryLimitExceeded(110, 100, "MLE"), ErrServiceMemoryLimitExceeded)
	s.ErrorIs(WrapErrTooManyRequests(100, "too many requests"), ErrServiceTooManyRequests)
	s.ErrorIs(WrapErrServiceInternal("never throw out"), ErrServiceInternal)
	s.ErrorIs(WrapErrServiceCrossClusterRouting("ins-0", "ins-1"), ErrServiceCrossClusterRouting)
	s.ErrorIs(WrapErrServiceDiskLimitExceeded(110, 100, "DLE"), ErrServiceDiskLimitExceeded)
	s.ErrorIs(WrapErrNodeNotMatch(0, 1, "SIM"), ErrNodeNotMatch)
	s.ErrorIs(WrapErrServiceUnimplemented(errors.New("mock grpc err")), ErrServiceUnimplemented)

	// Collection 相关错误。
	s.ErrorIs(WrapErrCollectionNotFound("test_collection", "failed to get collection"), ErrCollectionNotFound)
	s.ErrorIs(WrapErrCollectionNotLoaded("test_collection", "failed to query"), ErrCollectionNotLoaded)
	s.ErrorIs(WrapErrCollectionNotFullyLoaded("test_collection", "failed to query"), ErrCollectionNotFullyLoaded)
	s.ErrorIs(WrapErrCollectionNotLoaded("test_collection", "failed to alter index %s", "hnsw"), ErrCollectionNotLoaded)
	s.ErrorIs(WrapErrCollectionOnRecovering("test_collection", "channel lost %s", "dev"), ErrCollectionOnRecovering)
	s.ErrorIs(WrapErrCollectionVectorClusteringKeyNotAllowed("test_collection", "field"), ErrCollectionVectorClusteringKeyNotAllowed)
	s.ErrorIs(WrapErrCollectionSchemaMisMatch("schema mismatch", "field"), ErrCollectionSchemaMismatch)
	// Partition 相关错误。
	s.ErrorIs(WrapErrPartitionNotFound("test_partition", "failed to get partition"), ErrPartitionNotFound)
	s.ErrorIs(WrapErrPartitionNotLoaded("test_partition", "failed to query"), ErrPartitionNotLoaded)
	s.ErrorIs(WrapErrPartitionNotFullyLoaded("test_partition", "failed to query"), ErrPartitionNotFullyLoaded)

	// ResourceGroup 相关错误。
	s.ErrorIs(WrapErrResourceGroupNotFound("test_ResourceGroup", "failed to get ResourceGroup"), ErrResourceGroupNotFound)
	s.ErrorIs(WrapErrResourceGroupAlreadyExist("test_ResourceGroup", "failed to get ResourceGroup"), ErrResourceGroupAlreadyExist)
	s.ErrorIs(WrapErrResourceGroupReachLimit("test_ResourceGroup", 1, "failed to get ResourceGroup"), ErrResourceGroupReachLimit)
	s.ErrorIs(WrapErrResourceGroupIllegalConfig("test_ResourceGroup", nil, "failed to get ResourceGroup"), ErrResourceGroupIllegalConfig)
	s.ErrorIs(WrapErrResourceGroupNodeNotEnough("test_ResourceGroup", 1, 2, "failed to get ResourceGroup"), ErrResourceGroupNodeNotEnough)
	s.ErrorIs(WrapErrResourceGroupServiceAvailable("test_ResourceGroup", "failed to get ResourceGroup"), ErrResourceGroupServiceAvailable)

	// Replica 相关错误。
	s.ErrorIs(WrapErrReplicaNotFound(1, "failed to get replica"), ErrReplicaNotFound)
	s.ErrorIs(WrapErrReplicaNotAvailable(1, "failed to get replica"), ErrReplicaNotAvailable)

	// Channel 相关错误。
	s.ErrorIs(WrapErrChannelNotFound("test_Channel", "failed to get Channel"), ErrChannelNotFound)
	s.ErrorIs(WrapErrChannelLack("test_Channel", "failed to get Channel"), ErrChannelLack)
	s.ErrorIs(WrapErrChannelReduplicate("test_Channel", "failed to get Channel"), ErrChannelReduplicate)

	// Segment 相关错误。
	s.ErrorIs(WrapErrSegmentNotFound(1, "failed to get Segment"), ErrSegmentNotFound)
	s.ErrorIs(WrapErrSegmentNotLoaded(1, "failed to query"), ErrSegmentNotLoaded)
	s.ErrorIs(WrapErrSegmentLack(1, "lack of segment"), ErrSegmentLack)
	s.ErrorIs(WrapErrSegmentReduplicate(1, "redundancy of segment"), ErrSegmentReduplicate)
	s.ErrorIs(WrapErrSegmentRequestResourceFailed("Memory"), ErrSegmentRequestResourceFailed)

	// Node 相关错误。
	s.ErrorIs(WrapErrNodeNotFound(1, "failed to get node"), ErrNodeNotFound)
	s.ErrorIs(WrapErrNodeOffline(1, "failed to access node"), ErrNodeOffline)
	s.ErrorIs(WrapErrNodeLack(3, 1, "need more nodes"), ErrNodeLack)
	s.ErrorIs(WrapErrNodeStateUnexpected(1, "Stopping", "failed to suspend node"), ErrNodeStateUnexpected)

	// IO 相关错误。
	s.ErrorIs(WrapErrIoKeyNotFound("test_key", "failed to read"), ErrIoKeyNotFound)
	s.ErrorIs(WrapErrIoFailed("test_key", os.ErrClosed), ErrIoFailed)
	s.ErrorIs(WrapErrIoUnexpectEOF("test_key", os.ErrClosed), ErrIoUnexpectEOF)

	// 参数相关错误。
	s.ErrorIs(WrapErrParameterInvalid(8, 1, "failed to create"), ErrParameterInvalid)
	s.ErrorIs(WrapErrParameterInvalidRange(1, 1<<16, 0, "topk should be in range"), ErrParameterInvalid)
	s.ErrorIs(WrapErrParameterMissing("alias_name", "no alias parameter"), ErrParameterMissing)
	s.ErrorIs(WrapErrParameterTooLarge("unit test"), ErrParameterTooLarge)

	// 指标相关错误。
	s.ErrorIs(WrapErrMetricNotFound("unknown", "failed to get metric"), ErrMetricNotFound)

	// 消息队列相关错误。
	s.ErrorIs(WrapErrMqTopicNotFound("unknown", "failed to get topic"), ErrMqTopicNotFound)
	s.ErrorIs(WrapErrMqTopicNotEmpty("unknown", "topic is not empty"), ErrMqTopicNotEmpty)
	s.ErrorIs(WrapErrMqInternal(errors.New("unknown"), "failed to consume"), ErrMqInternal)

	// 字段相关错误。
	s.ErrorIs(WrapErrFieldNotFound("meta", "failed to get field"), ErrFieldNotFound)

	// 别名相关错误。
	s.ErrorIs(WrapErrAliasNotFound("alias", "failed to get collection id"), ErrAliasNotFound)
	s.ErrorIs(WrapErrCollectionIDOfAliasNotFound(1000, "failed to get collection id"), ErrCollectionIDOfAliasNotFound)

	// Search/Query 相关错误。
	s.ErrorIs(WrapErrInconsistentRequery("unknown"), ErrInconsistentRequery)
}

func (s *ErrSuite) TestCombine() {
	var (
		errFirst  = errors.New("first")
		errSecond = errors.New("second")
		errThird  = errors.New("third")
	)

	err := Combine(errFirst, errSecond)
	s.True(errors.Is(err, errFirst))
	s.True(errors.Is(err, errSecond))
	s.False(errors.Is(err, errThird))

	s.Equal("first: second", err.Error())
}

func (s *ErrSuite) TestCombineWithNil() {
	err := errors.New("non-nil")

	err = Combine(nil, err)
	s.NotNil(err)
}

func (s *ErrSuite) TestCombineOnlyNil() {
	err := Combine(nil, nil)
	s.Nil(err)
}

func (s *ErrSuite) TestCombineCode() {
	err := Combine(WrapErrPartitionNotFound(10), WrapErrCollectionNotFound(1))
	s.Equal(Code(ErrCollectionNotFound), Code(err))
}

func TestErrors(t *testing.T) {
	suite.Run(t, new(ErrSuite))
}
