### How to enable Jaeger tracing

If you want to enable Jaeger tracing for Porch:

* Apply the [deployment.yaml manifest](deployment.yaml) from this directory

```
kubectl apply -f deployment.yaml
```

* Enable trace export on the porch-server deployment:

```
kubectl edit deployment -n porch-system porch-server
```

Set the following environment variables:

```yaml
        env:
          - name: OTEL_TRACES_EXPORTER
            value: "otlp"
          - name: OTEL_EXPORTER_OTLP_TRACES_ENDPOINT
            value: "http://jaeger-oltp:4317"
          - name: OTEL_EXPORTER_OTLP_TRACES_PROTOCOL
            value: "grpc"
```

* Port-forward the Jaeger HTTP port to your local machine:

```
kubectl port-forward -n porch-system service/jaeger-http 16686
```

* Open your browser to the UI on http://localhost:16686

For full OpenTelemetry configuration options, see the [OpenTelemetry documentation](../../docs/content/en/docs/6_configuration_and_deployments/configurations/opentelemetry.md).
