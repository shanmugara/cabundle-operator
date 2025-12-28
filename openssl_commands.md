## Check the bundle
```kubectl get configmap omega-bundle -n default   -o jsonpath='{.data.root-certs\.pem}' | openssl crl2pkcs7 -nocrl -certfile /dev/stdin | openssl pkcs7 -print_certs -noout```