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

## Traffic shaping

## Firewalling

You can add rules to `solana-tpu-custom`. For instance if you wanted to temporarily close TPU port you can run:

```
iptables -I solana-tpu-custom -j DROP
```

This will drop all traffic to the tpu port.

If you would like to drop all traffic to TPU port apart from validators (staked nodes):

```
iptables -I solana-tpu-custom -m set --match-set solana-staked -j ACCEPT
iptables -I solana-tpu-custom -m set --match-set solana-highstaked -j ACCEPT
iptables -I solana-tpu-custom -j DROP
```

If you would only allow nodes in gossip to send to your TPU:

```
iptables -I solana-tpu-custom -m set --match-set solana-staked -j ACCEPT
iptables -I solana-tpu-custom -m set --match-set solana-highstaked -j ACCEPT
iptables -I solana-tpu-custom -m set --match-set solana-unstaked -j ACCEPT
iptables -I solana-tpu-custom -j DROP
```

