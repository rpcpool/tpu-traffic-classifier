# TPU traffic classifier


This small program creates ipsets and iptables rules for nodes in the Solana network.

By default, it creates and maintains the following ipsets:

 - `solana-unstaked`: unstaked nodes visible in gossip
 - `solana-staked`: staked nodes visible in gossip
 - `solana-high-staked`: nodes visible in gossip with >1% of stake

These sets will be kept up to date for as long as this software runs. On exit it will clean up the sets.

It also uses the PREROUTING tables to permanently mark traffic from these sets of IPs on the local nodes . This can be used in later traffic rules. By default the following fwmarks are set:

 - `1`: unstaked
 - `3`: staked
 - `9`: high staked

If you provide you validator pubkey it will assume that your validator runs on localhost and it will lookup the TPU port of the validator and enable the firwalling rules. If you do not provide your validator pubkey, all UDP traffic passing through this host will be passed through the chains created by this tool.

##  Running

Run: `go run ./main.go`
Build: `go build -o tpu-traffic-classifier ./main.go

```
$ ./tpu-traffic-classifier --help
Usage of ./tpu-traffic-classifier:
  -config-file string
        configuration file (default "config.yml")
  -pubkey string
        validator-pubkey
  -rpc-uri string
        the rpc uri to use (default "https://api.mainnet-beta.solana.com")
```

## Traffic shaping

You can use the fwmarks set by this tool to create traffic classes for QoS/traffic shaping.

## Firewalling

**If you do not provide a validator pubkey, then all UDP traffic will pass through this port**.

You can add rules to `solana-tpu-custom`. For instance if you wanted to temporarily close TPU port you can run:

```
iptables -I solana-tpu-custom -j DROP
```

This will drop all traffic to the tpu port.

If you would like to drop all traffic to TPU port apart from validators (staked nodes):

```
iptables -I solana-tpu-custom -m set --match-set solana-staked -j ACCEPT
iptables -I solana-tpu-custom -m set --match-set solana-high-staked -j ACCEPT
iptables -I solana-tpu-custom -j DROP
```

If you would only allow nodes in gossip to send to your TPU:

```
iptables -I solana-tpu-custom -m set --match-set solana-staked -j ACCEPT
iptables -I solana-tpu-custom -m set --match-set solana-high-staked -j ACCEPT
iptables -I solana-tpu-custom -m set --match-set solana-unstaked -j ACCEPT
iptables -I solana-tpu-custom -j DROP
```


## Example iptables generated chains

```
*filter
:INPUT ACCEPT [0:0]
:FORWARD DROP [0:0]
:OUTPUT ACCEPT [0:0]
:solana-tpu - [0:0]
:solana-tpu-custom - [0:0]
-A INPUT -p udp -m udp --dport 8004 -j solana-tpu
-A solana-tpu -j solana-tpu-custom
COMMIT
```

```
*mangle
:PREROUTING ACCEPT [0:0]
:solana-nodes - [0:0]
-A PREROUTING -p udp -m udp --dport 8004 -j solana-nodes
-A solana-nodes -m set --match-set solana-high-staked src -j MARK --set-xmark 0x9/0xffffffff
-A solana-nodes -m set --match-set solana-staked src -j MARK --set-xmark 0x3/0xffffffff
-A solana-nodes -m set --match-set solana-unstaked src -j MARK --set-xmark 0x1/0xffffffff
COMMIT
```
