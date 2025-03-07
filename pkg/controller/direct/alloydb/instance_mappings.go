// Copyright 2024 Google LLC
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//      http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

package alloydb

import (
	pb "cloud.google.com/go/alloydb/apiv1beta/alloydbpb"

	krm "github.com/GoogleCloudPlatform/k8s-config-connector/apis/alloydb/v1beta1"
	refs "github.com/GoogleCloudPlatform/k8s-config-connector/apis/refs/v1beta1"
	"github.com/GoogleCloudPlatform/k8s-config-connector/pkg/controller/direct"
)

func AlloyDBInstanceSpec_FromProto(mapCtx *direct.MapContext, in *pb.Instance) *krm.AlloyDBInstanceSpec {
	if in == nil {
		return nil
	}
	out := &krm.AlloyDBInstanceSpec{}
	out.Annotations = in.GetAnnotations()
	out.AvailabilityType = direct.Enum_FromProto(mapCtx, in.GetAvailabilityType())
	out.DatabaseFlags = in.GetDatabaseFlags()
	out.DisplayName = direct.LazyPtr(in.GetDisplayName())
	out.GCEZone = direct.LazyPtr(in.GetGceZone())
	out.InstanceTypeRef = Instance_InstanceType_FromProto(mapCtx, in.GetInstanceType())
	out.MachineConfig = Instance_MachineConfig_FromProto(mapCtx, in.GetMachineConfig())
	out.NetworkConfig = Instance_InstanceNetworkConfig_FromProto(mapCtx, in.GetNetworkConfig())
	out.ReadPoolConfig = Instance_ReadPoolConfig_FromProto(mapCtx, in.GetReadPoolConfig())
	return out
}

func AlloyDBInstanceSpec_ToProto(mapCtx *direct.MapContext, in *krm.AlloyDBInstanceSpec) *pb.Instance {
	if in == nil {
		return nil
	}
	out := &pb.Instance{}
	out.Annotations = in.Annotations
	out.AvailabilityType = direct.Enum_ToProto[pb.Instance_AvailabilityType](mapCtx, in.AvailabilityType)
	out.DatabaseFlags = in.DatabaseFlags
	out.DisplayName = direct.ValueOf(in.DisplayName)
	out.GceZone = direct.ValueOf(in.GCEZone)
	out.InstanceType = Instance_InstanceType_ToProto(mapCtx, in.InstanceTypeRef)
	out.MachineConfig = Instance_MachineConfig_ToProto(mapCtx, in.MachineConfig)
	out.NetworkConfig = Instance_InstanceNetworkConfig_ToProto(mapCtx, in.NetworkConfig)
	out.ReadPoolConfig = Instance_ReadPoolConfig_ToProto(mapCtx, in.ReadPoolConfig)
	return out
}

func AlloyDBInstanceStatus_FromProto(mapCtx *direct.MapContext, in *pb.Instance) *krm.AlloyDBInstanceStatus {
	if in == nil {
		return nil
	}
	out := &krm.AlloyDBInstanceStatus{}
	out.CreateTime = direct.StringTimestamp_FromProto(mapCtx, in.GetCreateTime())
	out.IPAddress = direct.LazyPtr(in.GetIpAddress())
	out.Name = direct.LazyPtr(in.GetName())
	out.OutboundPublicIPAddresses = in.GetOutboundPublicIpAddresses()
	out.PublicIPAddress = direct.LazyPtr(in.GetPublicIpAddress())
	out.Reconciling = direct.LazyPtr(in.Reconciling)
	out.State = direct.Enum_FromProto(mapCtx, in.GetState())
	out.Uid = direct.LazyPtr(in.Uid)
	out.UpdateTime = direct.StringTimestamp_FromProto(mapCtx, in.GetUpdateTime())

	return out
}

func Instance_InstanceType_FromProto(mapCtx *direct.MapContext, in pb.Instance_InstanceType) *refs.AlloyDBClusterTypeRef {
	out := &refs.AlloyDBClusterTypeRef{}
	instanceType := direct.Enum_FromProto(mapCtx, in)
	if instanceType == nil {
		return nil
	}
	out.External = *instanceType
	return out
}

func Instance_InstanceType_ToProto(mapCtx *direct.MapContext, in *refs.AlloyDBClusterTypeRef) pb.Instance_InstanceType {
	if in == nil {
		return direct.Enum_ToProto[pb.Instance_InstanceType](mapCtx, nil)
	}
	return direct.Enum_ToProto[pb.Instance_InstanceType](mapCtx, direct.PtrTo(in.External))
}
