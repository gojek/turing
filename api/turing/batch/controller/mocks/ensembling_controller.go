// Code generated by mockery v2.6.0. DO NOT EDIT.

package mocks

import (
	batchcontroller "github.com/gojek/turing/api/turing/batch/controller"
	mock "github.com/stretchr/testify/mock"

	models "github.com/gojek/turing/api/turing/models"
)

// EnsemblingController is an autogenerated mock type for the EnsemblingController type
type EnsemblingController struct {
	mock.Mock
}

// Create provides a mock function with given fields: request
func (_m *EnsemblingController) Create(request *batchcontroller.CreateEnsemblingJobRequest) error {
	ret := _m.Called(request)

	var r0 error
	if rf, ok := ret.Get(0).(func(*batchcontroller.CreateEnsemblingJobRequest) error); ok {
		r0 = rf(request)
	} else {
		r0 = ret.Error(0)
	}

	return r0
}

// GetStatus provides a mock function with given fields: namespace, ensemblingJob
func (_m *EnsemblingController) GetStatus(namespace string, ensemblingJob *models.EnsemblingJob) (batchcontroller.SparkApplicationState, error) {
	ret := _m.Called(namespace, ensemblingJob)

	var r0 batchcontroller.SparkApplicationState
	if rf, ok := ret.Get(0).(func(string, *models.EnsemblingJob) batchcontroller.SparkApplicationState); ok {
		r0 = rf(namespace, ensemblingJob)
	} else {
		r0 = ret.Get(0).(batchcontroller.SparkApplicationState)
	}

	var r1 error
	if rf, ok := ret.Get(1).(func(string, *models.EnsemblingJob) error); ok {
		r1 = rf(namespace, ensemblingJob)
	} else {
		r1 = ret.Error(1)
	}

	return r0, r1
}