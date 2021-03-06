/**
 * Licensed to the Apache Software Foundation (ASF) under one
 * or more contributor license agreements.  See the NOTICE file
 * distributed with this work for additional information
 * regarding copyright ownership.  The ASF licenses this file
 * to you under the Apache License, Version 2.0 (the
 * "License"); you may not use this file except in compliance
 * with the License.  You may obtain a copy of the License at
 *
 *     http://www.apache.org/licenses/LICENSE-2.0
 *
 * Unless required by applicable law or agreed to in writing, software
 * distributed under the License is distributed on an "AS IS" BASIS,
 * WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
 * See the License for the specific language governing permissions and
 * limitations under the License.
 */

package scheduler

import (
	"fmt"
	"github.com/gogo/protobuf/proto"
	mesos "github.com/mesos/mesos-go/mesosproto"
	util "github.com/mesos/mesos-go/mesosutil"
	"github.com/mesos/mesos-go/messenger"
	"github.com/mesos/mesos-go/upid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/suite"
	"os"
	"os/user"
	"testing"
	"time"
)

type SchedulerTestSuite struct {
	suite.Suite
	master      string
	masterUpid  string
	masterId    string
	frameworkID string
	framework   *mesos.FrameworkInfo
}

func (s *SchedulerTestSuite) SetupTest() {
	s.master = "127.0.0.1:8080"
	s.masterUpid = "master(2)@" + s.master
	s.masterId = "some-master-id-uuid"
	s.frameworkID = "some-framework-id-uuid"
	s.framework = util.NewFrameworkInfo(
		"test-user",
		"test-name",
		util.NewFrameworkID(s.frameworkID),
	)
}

func TestSchedulerSuite(t *testing.T) {
	suite.Run(t, new(SchedulerTestSuite))
}

func TestSchedulerDriverNew(t *testing.T) {
	masterAddr := "localhost:5050"
	mUpid, err := upid.Parse("master@" + masterAddr)
	assert.NoError(t, err)
	driver, err := NewMesosSchedulerDriver(NewMockScheduler(), &mesos.FrameworkInfo{}, masterAddr, nil)
	assert.NotNil(t, driver)
	assert.NoError(t, err)
	assert.True(t, driver.MasterPid.Equal(mUpid))
	user, _ := user.Current()
	assert.Equal(t, user.Username, driver.FrameworkInfo.GetUser())
	host, _ := os.Hostname()
	assert.Equal(t, host, driver.FrameworkInfo.GetHostname())
}

func (suite *SchedulerTestSuite) TestSchedulerDriverNew_WithFrameworkInfo_Override() {
	suite.framework.Hostname = proto.String("local-host")
	driver, err := NewMesosSchedulerDriver(NewMockScheduler(), suite.framework, "localhost:5050", nil)
	suite.NoError(err)
	suite.Equal(driver.FrameworkInfo.GetUser(), "test-user")
	suite.Equal("local-host", driver.FrameworkInfo.GetHostname())
}

func (suite *SchedulerTestSuite) TestSchedulerDriverStartOK() {
	sched := NewMockScheduler()

	messenger := messenger.NewMockedMessenger()
	messenger.On("Start").Return(nil)
	messenger.On("UPID").Return(&upid.UPID{})
	messenger.On("Send").Return(nil)
	messenger.On("Stop").Return(nil)

	driver, err := NewMesosSchedulerDriver(sched, suite.framework, suite.master, nil)
	driver.messenger = messenger
	suite.NoError(err)
	suite.True(driver.Stopped())

	stat, err := driver.Start()
	suite.NoError(err)
	suite.Equal(mesos.Status_DRIVER_RUNNING, stat)
	suite.False(driver.Stopped())
}

func (suite *SchedulerTestSuite) TestSchedulerDriverStartWithMessengerFailure() {
	sched := NewMockScheduler()
	sched.On("Error").Return()

	messenger := messenger.NewMockedMessenger()
	messenger.On("Start").Return(fmt.Errorf("Failed to start messenger"))
	messenger.On("Stop").Return()

	driver, err := NewMesosSchedulerDriver(sched, suite.framework, suite.master, nil)
	driver.messenger = messenger
	suite.NoError(err)
	suite.True(driver.Stopped())

	stat, err := driver.Start()
	suite.Error(err)
	suite.True(driver.Stopped())
	suite.True(!driver.Connected())
	suite.Equal(mesos.Status_DRIVER_NOT_STARTED, driver.Status())
	suite.Equal(mesos.Status_DRIVER_NOT_STARTED, stat)

}

func (suite *SchedulerTestSuite) TestSchedulerDriverStartWithRegistrationFailure() {
	sched := NewMockScheduler()
	sched.On("Error").Return()

	// Set expections and return values.
	messenger := messenger.NewMockedMessenger()
	messenger.On("Start").Return(nil)
	messenger.On("UPID").Return(&upid.UPID{})
	messenger.On("Send").Return(fmt.Errorf("messenger failed to send"))
	messenger.On("Stop").Return(nil)

	driver, err := NewMesosSchedulerDriver(sched, suite.framework, suite.master, nil)

	driver.messenger = messenger
	suite.NoError(err)
	suite.True(driver.Stopped())

	// reliable registration loops until the driver is stopped, connected, etc..
	stat, err := driver.Start()
	suite.NoError(err)
	suite.Equal(mesos.Status_DRIVER_RUNNING, stat)

	time.Sleep(5 * time.Second) // wait a bit, registration should be looping...

	suite.True(driver.Stopped())
	suite.Equal(mesos.Status_DRIVER_STOPPED, driver.Status())

	// stop the driver, should not panic!
	driver.Stop(false) // not failing over
	suite.True(driver.Stopped())
	suite.Equal(mesos.Status_DRIVER_STOPPED, driver.Status())

	messenger.AssertExpectations(suite.T())
}

func (suite *SchedulerTestSuite) TestSchedulerDriverJoinUnstarted() {
	driver, err := NewMesosSchedulerDriver(NewMockScheduler(), suite.framework, suite.master, nil)
	suite.NoError(err)
	suite.True(driver.Stopped())

	stat, err := driver.Join()
	suite.Error(err)
	suite.Equal(mesos.Status_DRIVER_NOT_STARTED, stat)
}

func (suite *SchedulerTestSuite) TestSchedulerDriverJoinOK() {
	// Set expections and return values.
	messenger := messenger.NewMockedMessenger()
	messenger.On("Start").Return(nil)
	messenger.On("UPID").Return(&upid.UPID{})
	messenger.On("Send").Return(nil)
	messenger.On("Stop").Return(nil)

	driver, err := NewMesosSchedulerDriver(NewMockScheduler(), suite.framework, suite.master, nil)
	driver.messenger = messenger
	suite.NoError(err)
	suite.True(driver.Stopped())

	stat, err := driver.Start()
	suite.NoError(err)
	suite.Equal(mesos.Status_DRIVER_RUNNING, stat)
	suite.False(driver.Stopped())

	testCh := make(chan mesos.Status)
	go func() {
		stat, _ := driver.Join()
		testCh <- stat
	}()

	close(driver.stopCh) // manually stopping
	stat = <-testCh      // when Stop() is called, stat will be DRIVER_STOPPED.
}

func (suite *SchedulerTestSuite) TestSchedulerDriverRun() {
	// Set expections and return values.
	messenger := messenger.NewMockedMessenger()
	messenger.On("Start").Return(nil)
	messenger.On("UPID").Return(&upid.UPID{})
	messenger.On("Send").Return(nil)
	messenger.On("Stop").Return(nil)

	driver, err := NewMesosSchedulerDriver(NewMockScheduler(), suite.framework, suite.master, nil)
	driver.messenger = messenger
	suite.NoError(err)
	suite.True(driver.Stopped())

	go func() {
		stat, err := driver.Run()
		suite.NoError(err)
		suite.Equal(mesos.Status_DRIVER_STOPPED, stat)
	}()
	time.Sleep(time.Millisecond * 1)

	suite.False(driver.Stopped())
	suite.Equal(mesos.Status_DRIVER_RUNNING, driver.Status())

	// close it all.
	driver.setStatus(mesos.Status_DRIVER_STOPPED)
	close(driver.stopCh)
	time.Sleep(time.Millisecond * 1)
}

func (suite *SchedulerTestSuite) TestSchedulerDriverStopUnstarted() {
	driver, err := NewMesosSchedulerDriver(NewMockScheduler(), suite.framework, suite.master, nil)
	suite.NoError(err)
	suite.True(driver.Stopped())

	stat, err := driver.Stop(true)
	suite.NotNil(err)
	suite.True(driver.Stopped())
	suite.Equal(mesos.Status_DRIVER_NOT_STARTED, stat)
}

func (suite *SchedulerTestSuite) TestSchdulerDriverStopOK() {
	// Set expections and return values.
	messenger := messenger.NewMockedMessenger()
	messenger.On("Start").Return(nil)
	messenger.On("UPID").Return(&upid.UPID{})
	messenger.On("Send").Return(nil)
	messenger.On("Stop").Return(nil)

	driver, err := NewMesosSchedulerDriver(NewMockScheduler(), suite.framework, suite.master, nil)
	driver.messenger = messenger
	suite.NoError(err)
	suite.True(driver.Stopped())

	go func() {
		stat, err := driver.Run()
		suite.NoError(err)
		suite.Equal(mesos.Status_DRIVER_STOPPED, stat)
	}()
	time.Sleep(time.Millisecond * 1)

	suite.False(driver.Stopped())
	suite.Equal(mesos.Status_DRIVER_RUNNING, driver.Status())

	driver.Stop(false)
	time.Sleep(time.Millisecond * 1)

	suite.True(driver.Stopped())
	suite.Equal(mesos.Status_DRIVER_STOPPED, driver.Status())
}

func (suite *SchedulerTestSuite) TestSchdulerDriverAbort() {
	// Set expections and return values.
	messenger := messenger.NewMockedMessenger()
	messenger.On("Start").Return(nil)
	messenger.On("UPID").Return(&upid.UPID{})
	messenger.On("Send").Return(nil)
	messenger.On("Stop").Return(nil)

	driver, err := NewMesosSchedulerDriver(NewMockScheduler(), suite.framework, suite.master, nil)
	driver.messenger = messenger
	suite.NoError(err)
	suite.True(driver.Stopped())

	go func() {
		stat, err := driver.Run()
		suite.NoError(err)
		suite.Equal(mesos.Status_DRIVER_ABORTED, stat)
	}()
	time.Sleep(time.Millisecond * 1)
	driver.setConnected(true) // simulated

	suite.False(driver.Stopped())
	suite.Equal(mesos.Status_DRIVER_RUNNING, driver.Status())

	stat, err := driver.Abort()
	time.Sleep(time.Millisecond * 1)
	suite.NoError(err)
	suite.True(driver.Stopped())
	suite.Equal(mesos.Status_DRIVER_ABORTED, stat)
	suite.Equal(mesos.Status_DRIVER_ABORTED, driver.Status())
}

func (suite *SchedulerTestSuite) TestSchdulerDriverLunchTasksUnstarted() {
	sched := NewMockScheduler()
	sched.On("Error").Return()

	// Set expections and return values.
	messenger := messenger.NewMockedMessenger()

	driver, err := NewMesosSchedulerDriver(sched, suite.framework, suite.master, nil)
	driver.messenger = messenger
	suite.NoError(err)
	suite.True(driver.Stopped())

	stat, err := driver.LaunchTasks(
		[]*mesos.OfferID{&mesos.OfferID{}},
		[]*mesos.TaskInfo{},
		&mesos.Filters{},
	)
	suite.Error(err)
	suite.Equal(mesos.Status_DRIVER_NOT_STARTED, stat)
}

func (suite *SchedulerTestSuite) TestSchdulerDriverLaunchTasksWithError() {
	sched := NewMockScheduler()
	sched.On("StatusUpdate").Return(nil)
	sched.On("Error").Return()

	msgr := messenger.NewMockedMessenger()
	msgr.On("Start").Return(nil)
	msgr.On("Send").Return(nil)
	msgr.On("UPID").Return(&upid.UPID{})
	msgr.On("Stop").Return(nil)

	driver, err := NewMesosSchedulerDriver(sched, suite.framework, suite.master, nil)
	driver.messenger = msgr
	suite.NoError(err)
	suite.True(driver.Stopped())

	go func() {
		driver.Run()
	}()
	time.Sleep(time.Millisecond * 1)
	driver.setConnected(true) // simulated
	suite.False(driver.Stopped())
	suite.Equal(mesos.Status_DRIVER_RUNNING, driver.Status())

	// to trigger error
	msgr2 := messenger.NewMockedMessenger()
	msgr2.On("Start").Return(nil)
	msgr2.On("UPID").Return(&upid.UPID{})
	msgr2.On("Send").Return(fmt.Errorf("Unable to send message"))
	msgr2.On("Stop").Return(nil)
	driver.messenger = msgr2

	// setup an offer
	offer := util.NewOffer(
		util.NewOfferID("test-offer-001"),
		suite.framework.Id,
		util.NewSlaveID("test-slave-001"),
		"test-slave(1)@localhost:5050",
	)

	pid, err := upid.Parse("test-slave(1)@localhost:5050")
	suite.NoError(err)
	driver.cache.putOffer(offer, pid)

	// launch task
	task := util.NewTaskInfo(
		"simple-task",
		util.NewTaskID("simpe-task-1"),
		util.NewSlaveID("test-slave-001"),
		[]*mesos.Resource{util.NewScalarResource("mem", 400)},
	)
	task.Command = util.NewCommandInfo("pwd")
	task.Executor = util.NewExecutorInfo(util.NewExecutorID("test-exec"), task.Command)
	tasks := []*mesos.TaskInfo{task}

	stat, err := driver.LaunchTasks(
		[]*mesos.OfferID{offer.Id},
		tasks,
		&mesos.Filters{},
	)
	suite.Error(err)
	suite.Equal(mesos.Status_DRIVER_RUNNING, stat)

}

func (suite *SchedulerTestSuite) TestSchdulerDriverLaunchTasks() {
	messenger := messenger.NewMockedMessenger()
	messenger.On("Start").Return(nil)
	messenger.On("UPID").Return(&upid.UPID{})
	messenger.On("Send").Return(nil)
	messenger.On("Stop").Return(nil)

	driver, err := NewMesosSchedulerDriver(NewMockScheduler(), suite.framework, suite.master, nil)
	driver.messenger = messenger
	suite.NoError(err)
	suite.True(driver.Stopped())

	go func() {
		driver.Run()
	}()
	time.Sleep(time.Millisecond * 1)
	driver.setConnected(true) // simulated
	suite.False(driver.Stopped())
	suite.Equal(mesos.Status_DRIVER_RUNNING, driver.Status())

	task := util.NewTaskInfo(
		"simple-task",
		util.NewTaskID("simpe-task-1"),
		util.NewSlaveID("slave-1"),
		[]*mesos.Resource{util.NewScalarResource("mem", 400)},
	)
	task.Command = util.NewCommandInfo("pwd")
	tasks := []*mesos.TaskInfo{task}

	stat, err := driver.LaunchTasks(
		[]*mesos.OfferID{&mesos.OfferID{}},
		tasks,
		&mesos.Filters{},
	)
	suite.NoError(err)
	suite.Equal(mesos.Status_DRIVER_RUNNING, stat)
}

func (suite *SchedulerTestSuite) TestSchdulerDriverKillTask() {
	messenger := messenger.NewMockedMessenger()
	messenger.On("Start").Return(nil)
	messenger.On("UPID").Return(&upid.UPID{})
	messenger.On("Send").Return(nil)
	messenger.On("Stop").Return(nil)

	driver, err := NewMesosSchedulerDriver(NewMockScheduler(), suite.framework, suite.master, nil)
	driver.messenger = messenger
	suite.NoError(err)
	suite.True(driver.Stopped())

	go func() {
		driver.Run()
	}()
	time.Sleep(time.Millisecond * 1)
	driver.setConnected(true) // simulated
	suite.False(driver.Stopped())
	suite.Equal(mesos.Status_DRIVER_RUNNING, driver.Status())

	stat, err := driver.KillTask(util.NewTaskID("test-task-1"))
	suite.NoError(err)
	suite.Equal(mesos.Status_DRIVER_RUNNING, stat)
}

func (suite *SchedulerTestSuite) TestSchdulerDriverRequestResources() {
	messenger := messenger.NewMockedMessenger()
	messenger.On("Start").Return(nil)
	messenger.On("UPID").Return(&upid.UPID{})
	messenger.On("Send").Return(nil)
	messenger.On("Stop").Return(nil)

	driver, err := NewMesosSchedulerDriver(NewMockScheduler(), suite.framework, suite.master, nil)
	driver.messenger = messenger
	suite.NoError(err)
	suite.True(driver.Stopped())

	driver.Start()
	driver.setConnected(true) // simulated
	suite.Equal(mesos.Status_DRIVER_RUNNING, driver.Status())

	stat, err := driver.RequestResources(
		[]*mesos.Request{
			&mesos.Request{
				SlaveId: util.NewSlaveID("test-slave-001"),
				Resources: []*mesos.Resource{
					util.NewScalarResource("test-res-001", 33.00),
				},
			},
		},
	)
	suite.NoError(err)
	suite.Equal(mesos.Status_DRIVER_RUNNING, stat)
}

func (suite *SchedulerTestSuite) TestSchdulerDriverDeclineOffers() {
	// see LaunchTasks test
}

func (suite *SchedulerTestSuite) TestSchdulerDriverReviveOffers() {
	messenger := messenger.NewMockedMessenger()
	messenger.On("Start").Return(nil)
	messenger.On("UPID").Return(&upid.UPID{})
	messenger.On("Send").Return(nil)
	messenger.On("Stop").Return(nil)

	driver, err := NewMesosSchedulerDriver(NewMockScheduler(), suite.framework, suite.master, nil)
	driver.messenger = messenger
	suite.NoError(err)
	suite.True(driver.Stopped())

	driver.Start()
	driver.setConnected(true) // simulated
	suite.Equal(mesos.Status_DRIVER_RUNNING, driver.Status())

	stat, err := driver.ReviveOffers()
	suite.NoError(err)
	suite.Equal(mesos.Status_DRIVER_RUNNING, stat)
}

func (suite *SchedulerTestSuite) TestSchdulerDriverSendFrameworkMessage() {
	messenger := messenger.NewMockedMessenger()
	messenger.On("Start").Return(nil)
	messenger.On("UPID").Return(&upid.UPID{})
	messenger.On("Send").Return(nil)
	messenger.On("Stop").Return(nil)

	driver, err := NewMesosSchedulerDriver(NewMockScheduler(), suite.framework, suite.master, nil)
	driver.messenger = messenger
	suite.NoError(err)
	suite.True(driver.Stopped())

	driver.Start()
	driver.setConnected(true) // simulated
	suite.Equal(mesos.Status_DRIVER_RUNNING, driver.Status())

	stat, err := driver.SendFrameworkMessage(
		util.NewExecutorID("test-exec-001"),
		util.NewSlaveID("test-slave-001"),
		"Hello!",
	)
	suite.NoError(err)
	suite.Equal(mesos.Status_DRIVER_RUNNING, stat)
}

func (suite *SchedulerTestSuite) TestSchdulerDriverReconcileTasks() {
	messenger := messenger.NewMockedMessenger()
	messenger.On("Start").Return(nil)
	messenger.On("UPID").Return(&upid.UPID{})
	messenger.On("Send").Return(nil)
	messenger.On("Stop").Return(nil)

	driver, err := NewMesosSchedulerDriver(NewMockScheduler(), suite.framework, suite.master, nil)
	driver.messenger = messenger
	suite.NoError(err)
	suite.True(driver.Stopped())

	driver.Start()
	driver.setConnected(true) // simulated
	suite.Equal(mesos.Status_DRIVER_RUNNING, driver.Status())

	stat, err := driver.ReconcileTasks(
		[]*mesos.TaskStatus{
			util.NewTaskStatus(util.NewTaskID("test-task-001"), mesos.TaskState_TASK_FINISHED),
		},
	)
	suite.NoError(err)
	suite.Equal(mesos.Status_DRIVER_RUNNING, stat)
}
