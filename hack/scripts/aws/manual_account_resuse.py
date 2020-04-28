#!/usr/bin/env python3
#
# This script written for python3.7
# If running on hive, you will need a virtualenv.
#
# $ virtualenv venv -p python3.7
# $ pip install boto3
#
# Then you should be able to run this script.

import argparse
import boto3

def assume_role(account_id, initial_session, verbose=False):
    client = initial_session.client('sts')
    role_arn = f"arn:aws:iam::{account_id}:role/OrganizationAccountAccessRole"
    role_session_name = "SREAdminReuseCleanup"
    duration = 900
    default_region='us-east-1'

    verbose and print(client.get_caller_identity())

    response = client.assume_role(
        RoleArn=role_arn,
        RoleSessionName=role_session_name,
        DurationSeconds=duration
    )

    session = boto3.Session(
        aws_access_key_id=response['Credentials']['AccessKeyId'],
        aws_secret_access_key=response['Credentials']['SecretAccessKey'],
        aws_session_token=response['Credentials']['SessionToken'],
        region_name=default_region
    )

    verbose and print(session)

    return session


def parse_arguments():
    parser = argparse.ArgumentParser()
    parser.add_argument(
        '-a',
        '--account_id',
        type=int,
        help='AWS AccountID to cleanup'
    )
    parser.add_argument(
        '-v',
        '--verbose',
        action='store_true',
        help='Enable verbose output'
    )

    return parser.parse_args()


def get_and_delete_s3_buckets(session, verbose=False):
    client = session.client('s3')
    bucket_count=0

    response = client.list_buckets()

    verbose and print(response)

    # S3 bucket responses do not paginate
    if response['Buckets']:
        for bucket in response['Buckets']:
            # This is where we delete buckets
            verbose and print(bucket['Name'])
            response = client.delete_bucket(Bucket=bucket)
            if response['ResponseMetadata']['HTTPStatusCode'] != 200:
                print(f"Failed deleting S3 Bucket: {bucket}")
            else:
                bucket_count += 1

    print(f"S3 Buckets deleted: {bucket_count}")

    return


def get_and_delete_ebs_volumes(client, verbose=False):
    volume_count=0
    paginator = client.get_paginator('describe_volumes')
    page_iterator = paginator.paginate()

    for page in page_iterator:
        verbose and print(page)
        for volume in page['Volumes']:
            # This is where we delete volumes
            verbose and print(volume)
            response = client.delete_volume(VolumeId=volume['Id'])
            if response['ResponseMetadata']['HTTPStatusCode'] != 200:
                print(f"Failed deleting VolumeId: {volume['Id']}")
            else:
                volume_count += 1

    print(f"EBS Volumes deleted: {volume_count}")

    return


def get_and_delete_snapshots(client, verbose=False):
    snapshot_filters=[
        {
            'Name': 'owner-alias',
            'Values': [
                'self'
            ]
        }
    ]

    snapshot_count=0

    paginator = client.get_paginator('describe_snapshots')
    page_iterator = paginator.paginate(Filters=snapshot_filters)

    for page in page_iterator:
        verbose and print(page)
        for snapshot in page['Snapshots']:
            # This is where we delete volumes
            verbose and print(snapshot)
            response = client.delete_snapshot(SnapshotId=snapshot['Id'])
            if response['ResponseMetadata']['HTTPStatusCode'] != 200:
                print(f"Failed deleting snapshotId: {snapshot['Id']}")
            else:
                snapshot_count += 1

    print(f"Snapshots deleted: {snapshot_count}")

    return


def get_and_delete_volumes_and_snapshots(session, verbose=False):
    client = session.client('ec2')
    get_and_delete_snapshots(client, verbose)
    get_and_delete_ebs_volumes(client, verbose)


def get_and_delete_hostedzone_recordsets(client, zone, verbose):
    record_set_paginator = client.get_paginator(
        'list_resource_record_sets')
    record_set_page_iterator = record_set_paginator.paginate(
        HostedZoneId=zone['Id'])

    for record_set_page in record_set_page_iterator:
        verbose and print(record_set_page)
        create_and_submit_change_batch(
            client,
            zone['Id'],
            record_set_page,
            verbose
        )


def create_and_submit_change_batch(client, zone_id, record_set_page, verbose):
    record_count = 0
    change_batch = {}
    change_batch['Changes'] = []
    for record_set in record_set_page['ResourceRecordSets']:
        verbose and print(record_set)
        if record_set['Type'] != 'NS' and record_set['Type'] != 'SOA':
            change_batch['Changes'].append(
                {
                    'Action': 'DELETE',
                    'ResourceRecordSet': record_set
                }
            )

    if change_batch['Changes']:
        pass
        # This is where we change resource record sets in patch
        response = client.change_resource_record_sets(
            HostedZoneId=zone_id,
            ChangeBatch=change_batch
        )
        if response['ResponseMetadata']['HTTPStatusCode'] != 200:
            print(f"Failed deleting record set batch: {change_batch}")
        else:
            record_count += len(change_batch['Changes'])

    print(f"Records deleted: {record_count}")

    return


def get_and_delete_route53_zones(session, verbose):
    client = session.client('route53')
    zone_count = 0

    zone_paginator = client.get_paginator('list_hosted_zones')
    zone_page_iterator = zone_paginator.paginate()


    for zone_page in zone_page_iterator:
        verbose and print(zone_page)
        for zone in zone_page['HostedZones']:
            verbose and print(zone)
            # There are two default zones that can't be deleted
            # So don't bother with the work if the zone count is two or less
            if zone['ResourceRecordSetCount'] > 2:
                get_and_delete_hostedzone_recordsets(client, zone, verbose)

            # This is where we delete the hosted zone
            response = client.delete_hosted_zone(Id=zone['Id'])
            if response['ResponseMetadata']['HTTPStatusCode'] != 200:
                print(f"Failed deleting zone: {zone['Id']}")
            else:
                zone_count += 1

    print(f"Zones deleted: {zone_count}")

    return


def main():
    args = parse_arguments()
    initial_session = boto3.Session(profile_name='osd-staging-1')

    # Assume the root account role for the accounts being cleaned up
    session = assume_role(args.account_id, initial_session, args.verbose)

    # Make sure the account is still there
    # validate_account()

    print(f"Deleting resources for account {args.account_id}")
    get_and_delete_s3_buckets(session, args.verbose)
    get_and_delete_volumes_and_snapshots(session, args.verbose)
    get_and_delete_route53_zones(session, args.verbose)


if __name__ == "__main__":
    main()