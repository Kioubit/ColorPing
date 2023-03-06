# ColorPing
![IPv6 Canvas example](screenshot.png?raw=true)

## How does it work?
Each IPv6 address in a /64 IPv6 subnet is associated to one pixel with color (RGB) information.  
When an address is pinged, the corresponding pixel is changed on the canvas and displayed
to all viewers via a webpage.

## Setup
Run and assign a /64 IPv6 subnet to the created interface named `canvas`.
The program needs to run as root or with the `CAP_NET_ADMIN` capability

### Example
```
./ColorPing
ip addr add fdcf:8538:9ad5:3333::1/64 dev canvas
ip link set up canvas
```
### Ping format
```
????:????:????:????:XXXX:YYYY:11RR:GGBB
```
Where:
- ``????`` can be anything
- ``XXXX`` must be the target X coordinate of the canvas, encoded as hexadecimal
- ``YYYY`` must be the target Y coordinate of the canvas, encoded as hexadecimal
- ``RR`` target "red" value (0-255), encoded as hexadecimal
- ``GG`` target "green" value (0-255), encoded as hexadecimal
- ``BB`` target "blue" value (0-255), encoded as hexadecimal