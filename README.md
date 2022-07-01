# TPU traffic classifier

**Use at your own risk: While this is tested to work well, it's early stage software used for testing and experiments.**

This small program creates ipsets and iptables rules for nodes in the Solana network. 

By default, it creates and maintains the following ipsets:

 - `solana-gossip`: all ips visible in gossip
 - `solana-unstaked`: unstaked nodes visible in gossip
 - `solana-staked`: staked nodes visible in gossip
 - `solana-high-staked`: nodes visible in gossip with >1% of stake

These sets will be kept up to date for as long as this software runs. On exit it will clean up the sets.

You can modify these categories by editing `config.yml`, setting the minimum stake percentages for each category. The validator will be placed in the largest category that applies to it.

It also uses the PREROUTING tables to permanently mark traffic from these sets of IPs on the local nodes . This can be used in later traffic rules. By default the following fwmarks are set:

 - `1`: unstaked
 - `3`: staked
 - `9`: high staked

If you provide you validator pubkey it will assume that your validator runs on localhost and it will lookup the TPU port of the validator and enable the firewalling rules. If you do not provide your validator pubkey, all UDP traffic passing through this host will be passed through the chains created by this tool.

##  Running

Run: `go run .`

Build: `go build -o tpu-traffic-classifier .`

```
Usage of ./tpu-traffic-classifier:
  -config-file string
        configuration file (default "config.yml")
  -fetch-identity
        fetch identity from rpc
  -fwd-policy string
        the default iptables policy for tpu forward, default is passthrough
  -our-localhost
        use localhost:8899 for rpc and fetch identity from that rpc
  -pubkey string
        validator-pubkey
  -rpc-uri string
        the rpc uri to use (default "https://api.mainnet-beta.solana.com")
  -tpu-policy string
        the default iptables policy for tpu, default is passthrough
  -tpu-quic-policy
        the default iptables policy for quic, default is passthrough
  -update
        whether or not to keep ipsets updated (default true)
  -vote-policy string
        the default iptables policy for votes, default is passthrough
```

## Recommended RPC node config

RPC nodes shouldn't expose TPU and TPUfwd (as they don't process TPU traffic into blocks) and should only receive traffic via sendTransaction.

You can use this tool to enforce this kind of firewall:

```
./tpu-traffic-classifier -config-file config.yml -our-localhost -tpu-policy DROP -fwd-policy DROP -tpu-quic-policy DROP -update=false
```

This mode will not keep the ipsets updated and will only create firewall rules for your RPC node to not accept traffic via TPU and TPUfwd.

## Sample config

```
# Special unstaked class for all nodes visible in gossip but without stake
unstaked_class:
  name: solana-unstaked
  fwmark: 1

# Different staked classes, the highest matching class will apply
staked_classes:
  - name: solana-staked
    stake_percentage: 0
    fwmark: 3
  - name: solana-high-staked 
    stake_percentage: 0.0003 # 100k stake - 0.03%
    fwmark: 9
```

## Firewalling

**If you do not provide a validator pubkey, then all UDP traffic will pass through these firewall rules**.

You can add rules to `solana-tpu-custom` (or `solana-tpu-custom-vote`, `solana-tpu-custom-fwd`). This chain will persist between invocations of this tool (it's not cleaned out). If you provide your validator pubkey, then the tool will look up your TPU port and send all incoming UDP TPU traffic to this port to the `solana-tpu-custom` chain.

For instance if you wanted to temporarily close TPU ports you can run:

```
iptables -A solana-tpu-custom -j DROP
```

This will drop all traffic to the tpu port.

If you would like to drop all traffic to TPU port apart from validators (staked nodes):

```
iptables -A solana-tpu-custom -m set --match-set solana-staked src -j ACCEPT
iptables -A solana-tpu-custom -m set --match-set solana-high-staked src -j ACCEPT
iptables -A solana-tpu-custom -j DROP
```

If you would only allow nodes in gossip to send to your TPU:

```
iptables -A solana-tpu-custom -m set --match-set solana-gossip src -j ACCEPT
iptables -A solana-tpu-custom -j DROP
```

Log all traffic from nodes not in gossip to you TPU fwd:

```
iptables -A solana-tpu-custom-fwd -m set ! --match-set solana-gossip src -j LOG --log-prefix 'TPUfwd:not in gossip:' --log-level info
```

These rules will only work when this utility is running. When it is not running, the TPU port will be open as usual.


## Rate limiting example

You can rate limit traffic to reduce the load on your TPU port:

```
#!/bin/bash

iptables -F solana-tpu-custom
# accept any amount of traffic from nodes with more than 100k stake:
iptables -A solana-tpu-custom -m set --match-set solana-high-staked src -j ACCEPT  
# accept 50/udp/second from low staked nodes
iptables -A solana-tpu-custom -m set --match-set solana-staked src -m limit --limit 50/sec -j ACCEPT                
# accept 1000 packets/second from RPC nodes (and other unstaked)
iptables -A solana-tpu-custom -m set --match-set solana-unstaked src -m limit --limit 1000/sec  -j ACCEPT # rpc nodes   
# accept 10 packets/second from nodes not visible in gossip
iptables -A solana-tpu-custom -m set ! --match-set solana-gossip src -m limit --limit 10/sec -j ACCEPT       
# log all dropped traffic (warning: lots of logs)
iptables -A solana-tpu-custom -j LOG --log-prefix "TPUport:" --log-level info
# drop everything that doesn't pass the limit
iptables -A solana-tpu-custom -j DROP

iptables -F solana-tpu-custom-fwd
# accept only forwarding traffic from nodes in gossip:
iptables -A solana-tpu-custom-fwd -m set --match-set solana-gossip src -j ACCEPT                                                                             
iptables -A solana-tpu-custom-fwd -j LOG --log-prefix "TPUfwd:" --log-level info                                                                             
iptables -A solana-tpu-custom-fwd -j DROP
```



## Traffic shaping

**Incomplete example, not usable as-is**

You can use the fwmarks set by this tool to create traffic classes for QoS/traffic shaping. You need to use IFB for incoming traffic filteringtraffic . 


```
tc qdisc add dev eth0 handle 1: ingress

tc filter add dev eth0 protocol ip parent 1: prio 1 handle 1 fw flowid 1:10 police rate 100mbit burst 100k # unstaked
tc filter add dev eth0 protocol ip parent 1: prio 1 handle 3 fw flowid 1:20 # staked
tc filter add dev eth0 protocol ip parent 1: prio 1 handle 9 fw flowid 1:30 # high staked
tc filter add dev eth0 protocol ip parent 1: prio 1 handle 6 fw flowid 1:40 # others
```


## Example iptables generated

The examples below is generated by this tool when run with the `pubkey` param for a valid validator. When the tool exits it will clean these rules up with the exception of `-custom...`  if (and only if) it's not empty.

```
*filter
:INPUT ACCEPT [0:0]
:FORWARD DROP [0:0]
:OUTPUT ACCEPT [0:0]
:solana-tpu - [0:0]
:solana-tpu-custom - [0:0]
-A INPUT -p udp -m udp --dport 8004 -j solana-tpu
-A INPUT -p udp -m udp --dport 8005 -j solana-tpu-fwd
-A INPUT -p udp -m udp --dport 8006 -j solana-tpu-vote
-A solana-tpu -j solana-tpu-custom
-A solana-tpu-fwd -j solana-tpu-custom-fwd
-A solana-tpu-vote -j solana-tpu-custom-vote
COMMIT
```

```
*mangle
:PREROUTING ACCEPT [0:0]
:solana-nodes - [0:0]
-A PREROUTING -p udp -m udp --dport 8004 -j solana-nodes
-A PREROUTING -p udp -m udp --dport 8005 -j solana-nodes
-A PREROUTING -p udp -m udp --dport 8006 -j solana-nodes
-A solana-nodes -m set --match-set solana-high-staked src -j MARK --set-xmark 0x9/0xffffffff
-A solana-nodes -m set --match-set solana-staked src -j MARK --set-xmark 0x3/0xffffffff
-A solana-nodes -m set --match-set solana-unstaked src -j MARK --set-xmark 0x1/0xffffffff
COMMIT
```
