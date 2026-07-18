# AWS coverage: networking / compute / storage

| Terraform type | Resource Explorer resourceType | Import ID format | Notes |
|---|---|---|---|
| aws_vpc | ec2:vpc | `vpc-id` | |
| aws_subnet | ec2:subnet | `subnet-id` | |
| aws_security_group | ec2:security-group | `sg-id` | |
| aws_security_group_rule | ec2:security-group-rule | `security_group_id_type_protocol_from_port_to_port_source` (underscore-joined; e.g. `sg-xxx_ingress_tcp_8000_8000_10.0.3.0/24`) | legacy/deprecated; struggles with multi-CIDR rules, prefer ingress/egress_rule resources below |
| aws_vpc_security_group_ingress_rule | ec2:security-group-rule | `sgr-id` | RE reports underlying `securitygroupingress` as `ec2:security-group-rule` |
| aws_vpc_security_group_egress_rule | ec2:security-group-rule | `sgr-id` | RE reports underlying `securitygroupegress` as `ec2:security-group-rule` |
| aws_route_table | ec2:route-table | `rtb-id` | |
| aws_route | ? | `route-table-id_destination` (CIDR/IPv6 CIDR/prefix-list-id) | sub-resource of route table; not RE-enumerable |
| aws_route_table_association | ? | `subnet-id/route-table-id` (or `gateway-id/route-table-id`) | sub-resource; not RE-enumerable |
| aws_internet_gateway | ec2:internet-gateway | `igw-id` | |
| aws_nat_gateway | ec2:natgateway | `nat-id` | RE type has no dash: `natgateway` |
| aws_egress_only_internet_gateway | ec2:egress-only-internet-gateway | `eigw-id` | |
| aws_eip | ec2:elastic-ip | `eipalloc-id` | RE type is `elastic-ip`, not `eip` |
| aws_network_acl | ec2:network-acl | `acl-id` | |
| aws_network_acl_rule | ? | `network_acl_id:rule_number:protocol:egress` | sub-resource; not RE-enumerable |
| aws_network_interface | ec2:network-interface | `eni-id` | |
| aws_network_interface_attachment | ? | `eni-attach-id` | sub-resource; not RE-enumerable |
| aws_vpc_endpoint | ec2:vpc-endpoint | `vpce-id` | |
| aws_vpc_endpoint_service | ? | `vpce-svc-id` | not RE-enumerable (absent from RE supported-types list) |
| aws_vpc_endpoint_route_table_association | ? | `vpc-endpoint-id/route-table-id` | sub-resource; not RE-enumerable |
| aws_vpc_endpoint_subnet_association | ? | `vpc-endpoint-id/subnet-id` | sub-resource; not RE-enumerable |
| aws_vpc_endpoint_security_group_association | ? | `vpc-endpoint-id/security-group-id` | sub-resource; not RE-enumerable |
| aws_vpc_peering_connection | ec2:vpc-peering-connection | `pcx-id` | |
| aws_vpc_peering_connection_accepter | ec2:vpc-peering-connection | `pcx-id` | manages accepter side of same underlying resource |
| aws_vpc_peering_connection_options | ? | `pcx-id` | sub-resource; not RE-enumerable |
| aws_ec2_transit_gateway | ec2:transit-gateway | `tgw-id` | |
| aws_ec2_transit_gateway_vpc_attachment | ec2:transit-gateway-attachment | `tgw-attach-id` | |
| aws_ec2_transit_gateway_peering_attachment | ec2:transit-gateway-attachment | `tgw-attach-id` | shares RE type with VPC attachment (no distinct "peering" type) |
| aws_ec2_transit_gateway_route_table | ec2:transit-gateway-route-table | `tgw-rtb-id` | |
| aws_ec2_transit_gateway_route | ? | `tgw-route-table-id_destination-cidr` | sub-resource; not RE-enumerable |
| aws_ec2_transit_gateway_route_table_association | ? | `tgw-route-table-id_tgw-attachment-id` | sub-resource; not RE-enumerable |
| aws_ec2_transit_gateway_route_table_propagation | ? | `tgw-route-table-id_tgw-attachment-id` | sub-resource; not RE-enumerable |
| aws_vpc_dhcp_options | ec2:dhcp-options | `dopt-id` | |
| aws_vpc_dhcp_options_association | ? | `vpc-id` | sub-resource (associates options set to VPC, ID is the VPC ID); not RE-enumerable |
| aws_ec2_managed_prefix_list | ec2:prefix-list | `pl-id` | |
| aws_ec2_managed_prefix_list_entry | ? | `pl-id,cidr` | sub-resource; not RE-enumerable |
| aws_flow_log | ec2:vpc-flow-log | `fl-id` | |
| aws_vpn_gateway | ec2:vpn-gateway | `vgw-id` | |
| aws_customer_gateway | ec2:customer-gateway | `cgw-id` | |
| aws_vpn_connection | ec2:vpn-connection | `vpn-id` | |
| aws_vpn_connection_route | ? | no import support | sub-resource; no Import section in provider docs |
| aws_instance | ec2:instance | `instance-id` | |
| aws_launch_template | ec2:launch-template | `lt-id` | |
| aws_key_pair | ec2:key-pair | `key_name` | import ID is the key pair name, not `key-...` id |
| aws_ami | ec2:image | `ami-id` | |
| aws_ami_copy | ec2:image | `ami-id` | creates a real AMI; same RE type/import as aws_ami |
| aws_ami_from_instance | ec2:image | `ami-id` | creates a real AMI; same RE type/import as aws_ami |
| aws_ebs_volume | ec2:volume | `vol-id` | |
| aws_volume_attachment | ? | `device_name:volume-id:instance-id` | sub-resource; not RE-enumerable |
| aws_ebs_snapshot | ec2:snapshot | `snap-id` | |
| aws_ebs_snapshot_copy | ec2:snapshot | `snap-id` (unverified) | no Import section found in provider docs; presumed same as aws_ebs_snapshot |
| aws_spot_instance_request | ec2:spot-instances-request | — | no import support (no Import section in provider docs); note RE type is plural "instances" |
| aws_spot_fleet_request | ec2:spot-fleet-request | `sfr-id` | |
| aws_placement_group | ec2:placement-group | `name` | |
| aws_ec2_capacity_reservation | ec2:capacity-reservation | `cr-id` | |
| aws_ec2_host | ec2:dedicated-host | `h-id` | RE type is `dedicated-host`, not `host` |
| aws_elb | elasticloadbalancing:loadbalancer | `name` | classic ELB |
| aws_lb | elasticloadbalancing:loadbalancer/app \| /net \| /gwy | `arn` | RE type varies by LB type (`app`=ALB, `net`=NLB, `gwy`=GWLB); `aws_alb` is an alias resource |
| aws_lb_target_group | elasticloadbalancing:targetgroup | `arn` | `aws_alb_target_group` is an alias resource |
| aws_lb_listener | elasticloadbalancing:listener/app \| /net \| /gwy | `arn` | `aws_alb_listener` is an alias resource |
| aws_lb_listener_rule | elasticloadbalancing:listener-rule/app | `arn` | ALB only; `aws_alb_listener_rule` is an alias resource |
| aws_lb_listener_certificate | ? | `listener-arn_certificate-arn` | sub-resource; not RE-enumerable |
| aws_lb_target_group_attachment | ? | `target-group-arn,target-id,port` | sub-resource (registration); not RE-enumerable |
| aws_autoscaling_group | autoscaling:autoScalingGroup | `name` | |
| aws_launch_configuration | ? | `name` | not RE-enumerable (absent from RE supported-types list, unlike ASG); import is supported |
| aws_autoscaling_policy | ? | `autoscaling-group-name/policy-name` | sub-resource; not RE-enumerable |
| aws_autoscaling_schedule | ? | `autoscaling-group-name/scheduled-action-name` | sub-resource; not RE-enumerable |
| aws_autoscaling_lifecycle_hook | ? | `autoscaling-group-name/lifecycle-hook-name` | sub-resource; not RE-enumerable |
| aws_s3_bucket | s3:bucket | `bucket name` | |
| aws_s3_bucket_versioning | ? | `bucket` or `bucket,expected_bucket_owner` | not RE-enumerable (part of parent bucket) |
| aws_s3_bucket_policy | ? | `bucket` | not RE-enumerable (part of parent bucket) |
| aws_s3_bucket_lifecycle_configuration | ? | `bucket` or `bucket,expected_bucket_owner` | not RE-enumerable (part of parent bucket) |
| aws_s3_bucket_server_side_encryption_configuration | ? | `bucket` or `bucket,expected_bucket_owner` | not RE-enumerable (part of parent bucket) |
| aws_s3_bucket_public_access_block | ? | `bucket` | not RE-enumerable (part of parent bucket) |
| aws_s3_bucket_ownership_controls | ? | `bucket` | not RE-enumerable (part of parent bucket) |
| aws_s3_bucket_cors_configuration | ? | `bucket` or `bucket,expected_bucket_owner` | not RE-enumerable (part of parent bucket) |
| aws_s3_bucket_website_configuration | ? | `bucket` or `bucket,expected_bucket_owner` | not RE-enumerable (part of parent bucket) |
| aws_s3_bucket_notification | ? | `bucket` | not RE-enumerable (part of parent bucket) |
| aws_s3_bucket_logging | ? | `bucket` or `bucket,expected_bucket_owner` | not RE-enumerable (part of parent bucket) |
| aws_s3_bucket_replication_configuration | ? | `bucket` | not RE-enumerable (part of parent bucket) |
| aws_s3_bucket_accelerate_configuration | ? | `bucket` or `bucket,expected_bucket_owner` | not RE-enumerable (part of parent bucket) |
| aws_s3_bucket_request_payment_configuration | ? | `bucket` or `bucket,expected_bucket_owner` | not RE-enumerable (part of parent bucket) |
| aws_s3_bucket_object_lock_configuration | ? | `bucket` or `bucket,account-id` | not RE-enumerable (part of parent bucket) |
| aws_s3_access_point | s3:accesspoint | `account-id:name` (or full ARN for S3-on-Outposts) | `aws_s3control_access_point` is an alias resource |
| aws_s3control_storage_lens_configuration | s3:storage-lens | `account-id:config-id` | |
| aws_efs_file_system | elasticfilesystem:file-system | `fs-id` | |
| aws_efs_mount_target | ? | `fsmt-id` | not RE-enumerable (absent from RE supported-types list) |
| aws_efs_access_point | elasticfilesystem:access-point | `fsap-id` | |
| aws_efs_backup_policy | ? | `fs-id` | sub-resource; not RE-enumerable |
| aws_efs_file_system_policy | ? | `fs-id` | sub-resource; not RE-enumerable |
| aws_fsx_windows_file_system | fsx:file-system | `fs-id` | |
| aws_fsx_lustre_file_system | fsx:file-system | `fs-id` | |
| aws_fsx_ontap_file_system | fsx:file-system | `fs-id` | |
| aws_fsx_openzfs_file_system | fsx:file-system | `fs-id` | |
| aws_fsx_backup | fsx:backup | `backup-id` | |
| aws_backup_vault | backup:backup-vault | `name` | |
| aws_backup_plan | backup:backup-plan | `id` | |
| aws_backup_selection | ? | `plan-id\|selection-id` | sub-resource; not RE-enumerable |
| aws_glacier_vault | glacier:vaults | `name` | RE type is plural "vaults" |
