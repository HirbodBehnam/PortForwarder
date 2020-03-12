# Port Forwarder
A small program to port forward with quota options
## What is this?
This a small program to forward ports with a quota option to control the data users use.
Some of the features of this code are:
* **Lightweight**: It has no dependencies, just the main file, and standard library.
* **Easy to use**: Just edit the rules file and you can use the proxy
* **High performance**: With iperf3 I achieved 14.9 Gbits/sec in a local tunnel.
* **Simultaneous Connections Limit**: Limit the amount of simultaneous _connections_ that a port can have.
* **Soft Blocking**: Block the new incoming connections and keep the old ones alive when the quota reaches. [Read More](#Soft-Blocking)

## Install
Just head to [releases](https://github.com/HirbodBehnam/PortForwarder/releases) and download one for your os.
### Build from source
Building this is not that hard. At first install [golang](https://golang.org/dl/) for your operating system. Clone this repository and run this command:
```bash
go build main.go
```
The executable file will be available at the present directory.
## How to use it?
Did you download the executable for your os? Good!

Edit the `rules.json` file as you wish. Here is the cheatsheet for it:
* `SaveDuration`: The program writes the quotas to disk every `SaveDuration` seconds. Default is 600 seconds or 10 minutes.
* `Timeout`: The time in seconds that a connection can stay alive without transmitting any data. Default is disabled. Use 0 to disable the timeout.
* `Rules` Array: Each element represents a forwarding rule and quota for it.
    * `Name`: Just a name for this rule. It does not have any effect on the application.
    * `Listen`: The local port to accept the incoming connections for proxy.
    * `Forward`: The address that the traffic must be forwarded to. Enter it like `ip:port`
    * `Quota`: The number of bytes the user can transfer.
    * `Simultaneous`: Amount of allowed simultaneous connections to this port. Use 0, or remove it for unlimited.
    * `ExpireDate`: When this rule must expire? In unix epoch, UTC. You can use [this site](https://www.epochconverter.com/) to convert date to unix epoch.
    
Save the file and just open the main executable to run the proxy.
### Unlimited Quota
Well, you can't but actually you can!

The max quota value is `92233720368547758087`. You can use this.
### Arguments
There are two options:
1. `-h`: It prints out the help of the proxy.
2. `--no-exit-save`: Disable the before exit rules saving
3. `-verbose`: Verbose mode (a number between 0 to 4)
4. `--config`: In case you want to use a config file with another name, just pass it to program as the first argument. For example:
```bash
./PortForwarder --config custom_conf.json
```
#### Verbose modes
You have 5 options

`0` means that the applications is mostly silent and prints fatal errors.

`1` means regular errors and infos are shown. (This is the default value)

`2` means it also prints when a user hits connection limit

`3` means it also prints a log when a connection timeouts

`4` means it prints every log possible. Use to debug

Example:
```bash
./PortForwarder --verbose 2
```
## How it works?
The base code is [this](https://gist.github.com/qhwa/cb9d3851450bff3b705e)(Thanks man!). The code is changed in order to measure the traffic transmitted.
### Soft Blocking
So here is a part you should read. The proxy uses the `io.Copy` function([reference](https://golang.org/pkg/io/#Copy)) in order to write the buffers. The good point is that the buffer is not with me and it is with the golang itself and there is no loop. But there is a catch: This function returns when it reaches the EOF or in case of an error.

So what's wrong with this? Well, I can understand how many bytes had been transferred when the function returns. So here comes soft connections and fast forward in cost of inaccuracy.

As soon as the function returns, I the quota will change.

And what do I mean about the softer connections? The client can use the program after the quota is reached. When the client wants to establish a new connection it will be rejected from the server. Plus you can manage how much client has used more than its quota.

## Other Stuff
[Persian guild to setup this with mtproto](http://rizy.ir/limitUsers)
