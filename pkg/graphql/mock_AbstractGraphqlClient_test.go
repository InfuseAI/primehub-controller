// Code generated by mockery v1.0.0. DO NOT EDIT.

package graphql

import mock "github.com/stretchr/testify/mock"

// MockAbstractGraphqlClient is an autogenerated mock type for the AbstractGraphqlClient type
type MockAbstractGraphqlClient struct {
	mock.Mock
}

// FetchByUserId provides a mock function with given fields: _a0
func (_m *MockAbstractGraphqlClient) FetchByUserId(_a0 string) (*DtoResult, error) {
	ret := _m.Called(_a0)

	var r0 *DtoResult
	if rf, ok := ret.Get(0).(func(string) *DtoResult); ok {
		r0 = rf(_a0)
	} else {
		if ret.Get(0) != nil {
			r0 = ret.Get(0).(*DtoResult)
		}
	}

	var r1 error
	if rf, ok := ret.Get(1).(func(string) error); ok {
		r1 = rf(_a0)
	} else {
		r1 = ret.Error(1)
	}

	return r0, r1
}

// FetchGroupEnableModelDeployment provides a mock function with given fields: _a0
func (_m *MockAbstractGraphqlClient) FetchGroupEnableModelDeployment(_a0 string) (bool, error) {
	ret := _m.Called(_a0)

	var r0 bool
	if rf, ok := ret.Get(0).(func(string) bool); ok {
		r0 = rf(_a0)
	} else {
		r0 = ret.Get(0).(bool)
	}

	var r1 error
	if rf, ok := ret.Get(1).(func(string) error); ok {
		r1 = rf(_a0)
	} else {
		r1 = ret.Error(1)
	}

	return r0, r1
}

// FetchGroupInfo provides a mock function with given fields: _a0
func (_m *MockAbstractGraphqlClient) FetchGroupInfo(_a0 string) (*DtoGroup, error) {
	ret := _m.Called(_a0)

	var r0 *DtoGroup
	if rf, ok := ret.Get(0).(func(string) *DtoGroup); ok {
		r0 = rf(_a0)
	} else {
		if ret.Get(0) != nil {
			r0 = ret.Get(0).(*DtoGroup)
		}
	}

	var r1 error
	if rf, ok := ret.Get(1).(func(string) error); ok {
		r1 = rf(_a0)
	} else {
		r1 = ret.Error(1)
	}

	return r0, r1
}

// FetchInstanceTypeInfo provides a mock function with given fields: _a0
func (_m *MockAbstractGraphqlClient) FetchInstanceTypeInfo(_a0 string) (*DtoInstanceType, error) {
	ret := _m.Called(_a0)

	var r0 *DtoInstanceType
	if rf, ok := ret.Get(0).(func(string) *DtoInstanceType); ok {
		r0 = rf(_a0)
	} else {
		if ret.Get(0) != nil {
			r0 = ret.Get(0).(*DtoInstanceType)
		}
	}

	var r1 error
	if rf, ok := ret.Get(1).(func(string) error); ok {
		r1 = rf(_a0)
	} else {
		r1 = ret.Error(1)
	}

	return r0, r1
}

// FetchTimeZone provides a mock function with given fields:
func (_m *MockAbstractGraphqlClient) FetchTimeZone() (string, error) {
	ret := _m.Called()

	var r0 string
	if rf, ok := ret.Get(0).(func() string); ok {
		r0 = rf()
	} else {
		r0 = ret.Get(0).(string)
	}

	var r1 error
	if rf, ok := ret.Get(1).(func() error); ok {
		r1 = rf()
	} else {
		r1 = ret.Error(1)
	}

	return r0, r1
}

// QueryServer provides a mock function with given fields: _a0
func (_m *MockAbstractGraphqlClient) QueryServer(_a0 map[string]interface{}) ([]byte, error) {
	ret := _m.Called(_a0)

	var r0 []byte
	if rf, ok := ret.Get(0).(func(map[string]interface{}) []byte); ok {
		r0 = rf(_a0)
	} else {
		if ret.Get(0) != nil {
			r0 = ret.Get(0).([]byte)
		}
	}

	var r1 error
	if rf, ok := ret.Get(1).(func(map[string]interface{}) error); ok {
		r1 = rf(_a0)
	} else {
		r1 = ret.Error(1)
	}

	return r0, r1
}
