#!/usr/bin/env python3
import json
import os 
import boto3
import uuid
import re
import subprocess
import sys

# Globals
amifile = '.github/workflows/amichange.json'
packerfile = ".github/packer/ubuntu-jammy-x86_64-public-ami.pkr.hcl"

# Environment Globals
product_id = os.getenv('PRODUCT_ID')
role_arn = os.getenv('ROLE_ARN')
vtag = os.getenv('TAG')
tag = vtag.replace('v', '')
skip_create_ami = os.getenv('SKIP_CREATE_AMI', "True")

def packer_build(packerfile):
  print("Running the packer build")
  output = subprocess.run('/usr/local/bin/packer build ' + packerfile, shell=True, stdout=subprocess.PIPE, stderr=subprocess.PIPE)
  if output.returncode != 0:
    raise RuntimeError(f"Command returned with code: {output.returncode}")

def packer_build_update(packerfile):
  print("Creating packer AMI image for Marketplace")
  output = subprocess.run('/usr/local/bin/packer build ' + packerfile, shell=True, stdout=subprocess.PIPE, stderr=subprocess.PIPE)
  if output.returncode != 0:
    raise RuntimeError(f"Command returned with code: {output.returncode}")

  found = re.findall('ami-[a-z0-9]*', str(output.stdout))

  if found:
    amiid = found[-1]
    return amiid
  else:
    raise RuntimeError(f"No AMI ID found in packer output: {output.stdout}")

def parse_amichange(amifile, amiid, role_arn, tag):
  # Create json blob to submit with the catalog update
  print("Updating the json artifact with recent amiid and tag information")
  with open(amifile, 'r') as file:
    data = json.load(file)

  data['DeliveryOptions'][0]['Details']['AmiDeliveryOptionDetails']['AmiSource']['AmiId']=amiid
  data['DeliveryOptions'][0]['Details']['AmiDeliveryOptionDetails']['AmiSource']['AccessRoleArn']=role_arn
  data['Version']['VersionTitle']=tag
  return json.dumps(data)

def update_ami(amifile, amiid):
  # Update the catalog with the last amiimage
  print('Updating the marketplace image')
  client = boto3.client('marketplace-catalog',region_name='us-east-1')
  uid = str(uuid.uuid4())
  global tag
  global product_id
  global role_arn

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
            'Details': parse_amichange(amifile,amiid,role_arn,tag),
            'ChangeName': 'Update'
          },
        ],
        ChangeSetName='AvalancheGo Update ' + tag,
        ClientRequestToken=uid
    )
    print(response)
  except client.exceptions.ResourceInUseException:
    print("The product is currently blocked by Amazon.  Please check the product site for more details")
  except Exception as e:
    print(f"An error occurred while updating AMI delivery options: {e}")

def main():
  try:
    if skip_create_ami == "True":
      packer_build(packerfile)
    else:
      update_ami(amifile, packer_build_update(packerfile))
    
    print("Ran packer build and update ami successfully")
  except Exception as e:
    print(f"An error occurred while running packer")
    sys.exit(5)

if __name__ == '__main__':
  main()

