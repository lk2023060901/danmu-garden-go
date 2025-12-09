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

package typeutil

import (
	"sync"
)

// UniqueSet 是只存储 UniqueID 的集合类型，
// 底层实现为 map[UniqueID]struct{}。
// 可以像创建 map 一样使用 make(UniqueSet) 创建实例。
type UniqueSet = Set[UniqueID]

func NewUniqueSet(ids ...UniqueID) UniqueSet {
	set := make(UniqueSet)
	set.Insert(ids...)
	return set
}

type Set[T comparable] map[T]struct{}

func NewSet[T comparable](elements ...T) Set[T] {
	set := make(Set[T])
	set.Insert(elements...)
	return set
}

// Insert 将元素插入集合。
// 如果元素已存在，则忽略该元素。
func (set Set[T]) Insert(elements ...T) {
	for i := range elements {
		set[elements[i]] = struct{}{}
	}
}

// Intersection 返回与给定集合的交集。
func (set Set[T]) Intersection(other Set[T]) Set[T] {
	ret := NewSet[T]()
	for elem := range set {
		if other.Contain(elem) {
			ret.Insert(elem)
		}
	}
	return ret
}

// Union 返回与给定集合的并集。
func (set Set[T]) Union(other Set[T]) Set[T] {
	ret := NewSet(set.Collect()...)
	ret.Insert(other.Collect()...)
	return ret
}

// Complement 返回相对于给定集合的差集（补集）。
func (set Set[T]) Complement(other Set[T]) Set[T] {
	if other == nil {
		return set
	}
	ret := NewSet(set.Collect()...)
	ret.Remove(other.Collect()...)
	return ret
}

// Contain 判断一个或多个元素是否都存在于集合中。
func (set Set[T]) Contain(elements ...T) bool {
	for i := range elements {
		_, ok := set[elements[i]]
		if !ok {
			return false
		}
	}
	return true
}

// Remove 从集合中移除元素。
// 如果集合为 nil 或元素不存在，则忽略。
func (set Set[T]) Remove(elements ...T) {
	for i := range elements {
		delete(set, elements[i])
	}
}

func (set Set[T]) Clear() {
	set.Remove(set.Collect()...)
}

// Collect 返回集合中所有元素的切片。
func (set Set[T]) Collect() []T {
	elements := make([]T, 0, len(set))
	for elem := range set {
		elements = append(elements, elem)
	}
	return elements
}

// Len 返回集合中元素的个数。
func (set Set[T]) Len() int {
	return len(set)
}

// Range 遍历集合中的所有元素。
// 当回调返回 false 时提前终止遍历。
func (set Set[T]) Range(f func(element T) bool) {
	for elem := range set {
		if !f(elem) {
			break
		}
	}
}

// Clone 返回一个拥有相同元素的新集合。
func (set Set[T]) Clone() Set[T] {
	ret := make(Set[T], set.Len())
	for elem := range set {
		ret.Insert(elem)
	}
	return ret
}

type ConcurrentSet[T comparable] struct {
	inner sync.Map
}

func NewConcurrentSet[T comparable]() *ConcurrentSet[T] {
	return &ConcurrentSet[T]{}
}

// Upsert 将元素插入并发集合。
// 如果元素已存在，则保持原状态不变。
func (set *ConcurrentSet[T]) Upsert(elements ...T) {
	for i := range elements {
		set.inner.Store(elements[i], struct{}{})
	}
}

func (set *ConcurrentSet[T]) Insert(element T) bool {
	_, exist := set.inner.LoadOrStore(element, struct{}{})
	return !exist
}

// Contain 判断一个或多个元素是否都存在于并发集合中。
func (set *ConcurrentSet[T]) Contain(elements ...T) bool {
	for i := range elements {
		_, ok := set.inner.Load(elements[i])
		if !ok {
			return false
		}
	}
	return true
}

// Remove 从并发集合中移除元素。
// 如果集合为 nil 或元素不存在，则忽略。
func (set *ConcurrentSet[T]) Remove(elements ...T) {
	for i := range elements {
		set.inner.Delete(elements[i])
	}
}

// TryRemove 试图从集合中移除单个元素。
// 如果元素不存在则返回 false。
func (set *ConcurrentSet[T]) TryRemove(element T) bool {
	_, exist := set.inner.LoadAndDelete(element)
	return exist
}

// Collect 返回并发集合中的所有元素。
func (set *ConcurrentSet[T]) Collect() []T {
	elements := make([]T, 0)
	set.inner.Range(func(key, value any) bool {
		elements = append(elements, key.(T))
		return true
	})
	return elements
}

func (set *ConcurrentSet[T]) Range(f func(element T) bool) {
	set.inner.Range(func(key, value any) bool {
		trueKey := key.(T)
		return f(trueKey)
	})
}
