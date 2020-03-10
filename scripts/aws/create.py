import sys
import boto3

ec2 = boto3.client("ec2")

# Should be called with python3 aws_create.py $numBootstraps $numNodes
numBootstraps = int(sys.argv[1])
numNodes = int(sys.argv[2])

bootstapNode = "Borealis-Bootstrap"
fullNode = "Borealis-Node"


def runInstances(num: int, name: str):
    if num > 0:
        ec2.run_instances(
            ImageId="ami-0badd1c10cb7673e9",
            InstanceType="c5.large",
            MaxCount=num,
            MinCount=num,
            SubnetId="subnet-0c80cf240e54118c8",
            TagSpecifications=[
                {"ResourceType": "instance", "Tags": [{"Key": "Name", "Value": name}]}
            ],
            SecurityGroupIds=["sg-0d6172e416170b426"],
            KeyName="stephen_ava",
        )


def main():
    runInstances(numBootstraps, bootstapNode)
    runInstances(numNodes, fullNode)


if __name__ == "__main__":
    main()
