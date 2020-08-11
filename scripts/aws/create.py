#!/usr/bin/env python3
"""
Start a number of Avalanche nodes on Amazon EC2
"""

import boto3


bootstapNode = "Cascade-Bootstrap"
fullNode = "Cascade-Node"


def runInstances(ec2, num: int, name: str):
    if num > 0:
        ec2.run_instances(
            ImageId="ami-0c088c21fbcdb8a48",
            InstanceType="c5.large",
            MaxCount=num,
            MinCount=num,
            SubnetId="subnet-0c80cf240e54118c8",
            TagSpecifications=[
                {"ResourceType": "instance", "Tags": [{"Key": "Name", "Value": name}]}
            ],
            SecurityGroupIds=["sg-0d6172e416170b426"],
            KeyName="stephen_ava", # TODO rename key
        )


def main():
    import argparse

    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("numBootstraps", type=int)
    parser.add_argument("numNodes", type=int)
    args = parser.parse_args()

    ec2 = boto3.client("ec2")
    runInstances(ec2, args.numBootstraps, bootstapNode)
    runInstances(ec2, args.numNodes, fullNode)


if __name__ == "__main__":
    main()
