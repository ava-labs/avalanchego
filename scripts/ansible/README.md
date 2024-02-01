# Ansible for Camino-Node

[Ansible](https://ansible.com) playbooks, roles, & inventories to install
[Camino-Node](https://github.com/chain4travel/camino-node) as a systemd service.
Target(s) can be

- localhost
- Cloud VMs (e.g. Amazon, Azure, Digital Ocean)
- Raspberry Pi
- any host running a supported operating system
- any combination of the above


## Using

To create a Camino-Node service on localhost

1. Check you have Ansible 2.9+ (see [Installing](#installing))
2. Clone the Camino-Node git repository
    ```
    $ git clone https://github.com/chain4travel/camino-node
    ```

3. Change to this directory
    ```
    $ cd camino-node/scripts/ansible
    ```

4. Run the service playbook
    ```
    $ ansible-playbook \
        -i inventories/localhost.yml \
        --ask-become-pass \
        service_playbook.yml
    ```

   You don't need `--ask-become-pass` if your account doesn't require a sudo
   password. To install on remote hosts you will need to create an inventory,
   see [customising].

   Output should look similar to the [example run](#example-run).

5. Check the service is running
    ```
    $ systemctl status camino-node
    ```

    The output should look similar to
    ```
    ● camino-node.service - Camino-Node node for Camino consensus network
    Loaded: loaded (/etc/systemd/system/camino-node.service; enabled; vendor preset: enabled)
    Active: active (running) since Wed 2020-10-21 10:00:00 UTC; 32s ago
    ...
    ```


## Installing

Ansible 2.9 (or higher) is required. To check, run

```
ansible --version
```

the first line of output should be like `ansible 2.9.x`, or `ansible 2.10.x`
(`x` can be any number). If the output includes `ansible: command not found`,
or an earlier version (e.g. `ansible 2.8.16`), then you need to install a
supported version.

To install a supported version

1. Create a Python Virtualenv
    ```
    $ python3 -m venv venv/
    ```

2. Activate the Virtualenv
    ```
    $ source venv/bin/activate
    ```

4. Install Ansible inside the virtualenv
    ```
    $ pip install "ansible>=2.9"
    ```


## Customising

To run against remote targets you'll need an [Ansible inventory](https://docs.ansible.com/ansible/latest/user_guide/intro_inventory.html#inventory-basics-formats-hosts-and-groups).
Here are some examples to use as a starting point.

### Amazon

```yaml
camino_nodes:
  hosts:
    ec2-203-0-113-42.us-east-1.compute.amazonaws.com:
    ec2-203-0-113-9.ap-southeast-1.compute.amazonaws.com:
  vars:
    ansible_ssh_private_key_file: "~/.ssh/aws_identity.pem"
    ansible_user: "ubuntu"
```

### Raspberry Pi

```yaml
camino_nodes:
  hosts:
    raspberrypi.local:
  vars:
    ansible_user: "pi"
```

## Requirements

Target operating systems supported by these roles & playbooks are

- CentOS 7
- CentOS 8
- Debian 10
- Raspberry Pi OS
- Ubuntu 18.04 LTS
- Ubuntu 20.04 LTS


## Example run

```
PLAY [Configure Camino service] *********************************************

TASK [Gathering Facts] *********************************************************
ok: [localhost]

TASK [camino_download : Query releases] *************************************
ok: [localhost]

TASK [camino_download : Fetch release] **************************************
changed: [localhost] => (item=camino-node-linux-arm64-v0.0.0.tar.gz)
changed: [localhost] => (item=camino-node-linux-arm64-v0.0.0.tar.gz.sig)

TASK [camino_download : Create temp gnupghome] ******************************
changed: [localhost]

TASK [camino_download : Import keys] ****************************************
changed: [localhost]

TASK [camino_download : Verify signature] ***********************************
ok: [localhost]

TASK [camino_download : Cleanup temp gnupghome] *****************************
changed: [localhost]

TASK [camino_download : Unpack release] *************************************
changed: [localhost] => (item=camino-node-linux-arm64-v1.5.0.tar.gz)

TASK [camino_user : Create Camino daemon group] **************************
changed: [localhost]

TASK [camino_user : Create Camino daemon user] ***************************
changed: [localhost]

TASK [camino_install : Create shared directories] ***************************
changed: [localhost] => (item={'path': '/usr/local/bin'})
changed: [localhost] => (item={'path': '/var/local/lib'})
changed: [localhost] => (item={'path': '/var/local/log'})

TASK [camino_install : Create Camino directories] ************************
changed: [localhost] => (item=/var/local/lib/camino-node)
changed: [localhost] => (item=/var/local/lib/camino-node/db)
changed: [localhost] => (item=/var/local/lib/camino-node/staking)
changed: [localhost] => (item=/var/local/log/camino-node)
changed: [localhost] => (item=/usr/local/lib/camino-node)

TASK [camino_install : Install Camino binary] ****************************
changed: [localhost]

TASK [camino_install : Remove outdated support files] **********************
ok: [localhost] => (item={'path': '/usr/local/lib/camino-node/evm'})
ok: [localhost] => (item={'path': '/usr/local/lib/camino-node/camino-node-preupgrade'})
ok: [localhost] => (item={'path': '/usr/local/lib/camino-node/camino-node-latest'})

TASK [camino_install : Install support files] *******************************
changed: [localhost] => (item=/usr/local/lib/camino-node/plugins)

TASK [camino_staker : Create staking key] ***********************************
changed: [localhost]

TASK [camino_staker : Create staking certificate signing request] ***********
changed: [localhost]

TASK [camino_staker : Create staking certificate] ***************************
changed: [localhost]

TASK [camino_service : Configure Camino service] *************************
changed: [localhost]

TASK [camino_service : Enable Camino service] ****************************
changed: [localhost]

RUNNING HANDLER [camino_service : Restart Camino service] ****************
changed: [localhost]

PLAY RECAP *********************************************************************
localhost : ok=6 changed=24 unreachable=0 failed=0 skipped=0 rescued=0 ignored=0
```
