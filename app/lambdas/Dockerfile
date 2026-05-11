# syntax=docker/dockerfile:1.6

# Generation-worker Lambda image: Lambda Java 21 base + Python 3 + notebooklm-py
# so the worker can shell out to scripts/notebooklm/overview.py.
# Requires `make build-lambdas` first; scripts/ comes in as a buildx context.

FROM public.ecr.aws/lambda/java:21

RUN microdnf install -y python3 python3-pip \
    && microdnf clean all \
    && python3 -m venv /opt/notebooklm-venv \
    && /opt/notebooklm-venv/bin/pip install --no-cache-dir --upgrade pip

COPY --from=scripts notebooklm/requirements.txt /tmp/notebooklm-requirements.txt
RUN /opt/notebooklm-venv/bin/pip install --no-cache-dir -r /tmp/notebooklm-requirements.txt \
    && rm /tmp/notebooklm-requirements.txt

COPY --from=scripts notebooklm/overview.py /var/task/scripts/notebooklm/overview.py
COPY target/media-service-lambdas.jar ${LAMBDA_TASK_ROOT}/lib/

ENV NOTEBOOKLM_PYTHON=/opt/notebooklm-venv/bin/python3 \
    NOTEBOOKLM_SCRIPT_PATH=/var/task/scripts/notebooklm/overview.py

CMD ["com.mediaservice.lambda.GenerationWorkerHandler::handleRequest"]
