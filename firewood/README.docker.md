# Docker on Mac Compatibility

Note:
Docker compatiblitiy is a work in progress. Please PR any changes here if you find a better way to do this.

## Steps

### Step 1

Install docker-desktop ([guide](https://docs.docker.com/desktop/install/mac-install/))

### Step 2

Setup a dev-environment ([guide](https://docs.docker.com/desktop/dev-environments/set-up/#set-up-a-dev-environment))

Here, you want to specifically pick a local-directory (the repo's directory)

![image](https://github.com/ava-labs/firewood/assets/3286504/83d6b66d-19e3-4b59-bc73-f67cf68d7329)

This is best because you can still do all your `git` stuff from the host.

### Step 3

You will need the `Dev Containers` VSCODE extension, authored by Microsoft for this next step.

Open your dev-environment with VSCODE. Until you do this, the volume might not be properly mounted. If you (dear reader) know of a better way to do this, please open a PR. VSCODE is very useful for its step-by-step debugger, but other than that, you can run whatever IDE you would like in the host environment and just open a shell in the container to run the tests.

![image](https://github.com/ava-labs/firewood/assets/3286504/88c981cb-42b9-4b99-acec-fbca31cca652)

### Step 4

Open a terminal in vscode OR exec into the container directly as follows

```sh
# you don't need to do this if you open the terminal from vscode
# the container name here is "firewood-app-1", you should be able to see this in docker-desktop
docker exec -it --privileged -u root firewood-app-1 zsh
```

Once you're in the terminal you'll want to install the Rust toolset. You can [find instructions here](https://rustup.rs/)

**!!! IMPORTANT !!!**

Make sure you read the output of any commands that you run. `rustup` will likely ask you to `source` a file to add some tools to your `PATH`.

You'll also need to install all the regular linux dependencies (if there is anything from this list that's missing, please add to this README)

```sh
apt update
apt install vim
apt install build-essential
apt install protobuf-compiler
```

### Step 5

**!!! IMPORTANT !!!**

You need to create a separate `CARGO_TARGET_DIR` that isn't volume mounted onto the host. `VirtioFS` (the default file-system) has some concurrency issues when dealing with sequential writes and reads to a volume that is mounted to the host. You can put a directory here for example: `/root/target`.

For step-by-step debugging and development directly in the container, you will also **need to make sure that `rust-analyzer` is configured to point to the new target-directory instead of just default**.

There are a couple of places where this can be setup. If you're a `zsh` user, you should add `export CARGO_TARGET_DIR=/root/target` to either `/root/.zshrc` or `/root/.bashrc`.
After adding the line, don't forget to `source` the file to make sure your current session is updated.

### Step 6

Navigate to `/com.docker.devenvironments.code` and run `cargo test`. If it worked, you are most of the way there! If it did not work, there are a couple of common issues. If the code will not compile, it's possible that your target directory isn't set up properly. Check inside `/root/target` to see if there are any build artifacts. If not, you might need to call `source ~/.zshrc` again (sub in whatever your preferred shell is).

Now for vscode, you need to configure your `rust-analyzer` in the "remote-environment" (the Docker container). There are a couple of places to do this. First, you want to open `/root/.vscode-server/Machine/settings.json` and make sure that you have the following entry:

```json
{
  "rust-analyzer.cargo.extraEnv": {
    "CARGO_TARGET_DIR": "/root/target"
  }
}
```

Then, you want to make sure that the terminal that's being used by the vscode instance (for the host system) is the same as your preferred terminal in the container to make sure that things work as expected. [Here are the docs](https://code.visualstudio.com/docs/terminal/profiles) to help you with setting up the proper profile.

And that should be enough to get your started! Feel free to open an issue if you need any help debugging.
