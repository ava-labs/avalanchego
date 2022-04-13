#!/usr/bin/env python3
"""
Start a number of Camino nodes on Amazon EC2
"""

import boto3


bootstapNode = "Cascade-Bootstrap"
fullNode = "Cascade-Node"


def runInstances(ec2, num: int, name: str):
    if num > 0:
        ec2.run_instances(
            ImageId="ami-XXX",
            InstanceType="c5.large",
            MaxCount=num,
            MinCount=num,
            SubnetId="subnet-XXX",
            TagSpecifications=[
                {"ResourceType": "instance", "Tags": [{"Key": "Name", "Value": name}]}
            ],
            SecurityGroupIds=["sg-XXX"],
            KeyName="ZZZ",
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
