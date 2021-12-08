This script is created to export the OTS loss values for the connections with Raman Amplifier(RA2P).

Usage:

```
./otsloss_linux_x64.exe -u admin -p "PASSWORD" -i 172.172.172.172 -l ASWG
```
Where ASWG is the Line Driver which we want to use as a filter critria so that only the OTS connections with at least 1x ASWG card will be chosen for the calculation.
