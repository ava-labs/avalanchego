#!/usr/bin/python3
import json
import os 
import boto3
import uuid
import re
import packer

uid = str(uuid.uuid4())
file = '.github/workflows/amichange.json'
packerfile = ".github/packer/ubuntu-focal-x86_64-public-ami.json"

product_id = os.getenv('PRODUCT_ID')
role_arn = os.getenv('ROLE_ARN')
vtag = os.getenv('TAG')
tag = vtag.replace('v', '')
variables = [product_id,role_arn,tag]

for var in variables:
  if var is None:
    print("A Variable is not set correctly or this is not the right repo.  Exiting.")
    exit(0)
  
client = boto3.client('marketplace-catalog',region_name='us-east-1')

def packer_build(packerfile):
  p = packer.Packer(packerfile)
  output = p.build(parallel=False, debug=False, force=False)
  found = re.findall('ami-[a-z0-9]*', str(output))
  return found[-1]

def parse_amichange(object):
  with open(object, 'r') as file:
    data = json.load(file)

  data['DeliveryOptions'][0]['Details']['AmiDeliveryOptionDetails']['AmiSource']['AmiId']=amiid
  data['DeliveryOptions'][0]['Details']['AmiDeliveryOptionDetails']['AmiSource']['AccessRoleArn']=role_arn
  data['Version']['VersionTitle']=tag
  return json.dumps(data)

amiid=packer_build(packerfile)

try:
  response = client.start_change_set(
    Catalog='AWSMarketplace',
    ChangeSet=[
      {
        'ChangeType': 'AddDeliveryOptions',
        'Entity': {
          'Type': 'AmiProduct@1.0',
          'Identifier': product_id
        },
          'Details': parse_amichange(file),
          'ChangeName': 'Update'
        },
      ],
      ChangeSetName='AvalancheGo Update ' + tag,
      ClientRequestToken=uid
  )
  print(response)
except client.exceptions.ResourceInUseException:
  print("The product is currently blocked by Amazon.  Please check the product site for more details")

