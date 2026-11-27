/*
Copyright The Velero Contributors.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

package datapath

import (
	"context"
	"sync"

	"github.com/pkg/errors"
	"github.com/sirupsen/logrus"
	"k8s.io/client-go/kubernetes"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/manager"
)

var ConcurrentLimitExceed error = errors.New("Concurrent number exceeds")
var FSBRCreator = newFileSystemBR
var MicroServiceBRWatcherCreator = newMicroServiceBRWatcher

type Manager struct {
	cocurrentNum int
	trackerLock  sync.Mutex
	tracker      map[string]AsyncBR
	released     map[string]bool
}

// NewManager creates the data path manager to manage concurrent data path instances
func NewManager(cocurrentNum int) *Manager {
	return &Manager{
		cocurrentNum: cocurrentNum,
		tracker:      map[string]AsyncBR{},
		released:     map[string]bool{},
	}
}

// CreateFileSystemBR creates a new file system backup/restore data path instance
func (m *Manager) CreateFileSystemBR(jobName string, requestorType string, ctx context.Context, client client.Client, namespace string, callbacks Callbacks, log logrus.FieldLogger) (AsyncBR, error) {
	m.trackerLock.Lock()
	defer m.trackerLock.Unlock()

	if len(m.tracker)-len(m.released) >= m.cocurrentNum {
		return nil, ConcurrentLimitExceed
	}

	m.tracker[jobName] = FSBRCreator(jobName, requestorType, client, namespace, callbacks, log)
	delete(m.released, jobName)

	return m.tracker[jobName], nil
}

// CreateMicroServiceBRWatcher creates a new micro service watcher instance
func (m *Manager) CreateMicroServiceBRWatcher(ctx context.Context, client client.Client, kubeClient kubernetes.Interface, mgr manager.Manager, taskType string,
	taskName string, namespace string, podName string, containerName string, associatedObject string, callbacks Callbacks, resume bool, log logrus.FieldLogger) (AsyncBR, error) {
	m.trackerLock.Lock()
	defer m.trackerLock.Unlock()

	if !resume {
		if len(m.tracker)-len(m.released) >= m.cocurrentNum {
			return nil, ConcurrentLimitExceed
		}
	}

	m.tracker[taskName] = MicroServiceBRWatcherCreator(client, kubeClient, mgr, taskType, taskName, namespace, podName, containerName, associatedObject, callbacks, log)
	delete(m.released, taskName)

	return m.tracker[taskName], nil
}

// RemoveAsyncBR removes a file system backup/restore data path instance
func (m *Manager) RemoveAsyncBR(jobName string) {
	m.trackerLock.Lock()
	defer m.trackerLock.Unlock()

	delete(m.tracker, jobName)
	delete(m.released, jobName)
}

func (m *Manager) ReleaseAsyncBRSlot(jobName string) {
	m.trackerLock.Lock()
	defer m.trackerLock.Unlock()

	if _, exists := m.tracker[jobName]; exists {
		if m.released == nil {
			m.released = map[string]bool{}
		}
		m.released[jobName] = true
	}
}

// GetAsyncBR returns the file system backup/restore data path instance for the specified job name
func (m *Manager) GetAsyncBR(jobName string) AsyncBR {
	m.trackerLock.Lock()
	defer m.trackerLock.Unlock()

	if async, exist := m.tracker[jobName]; exist {
		return async
	} else {
		return nil
	}
}
