pipeline {
    agent any

    environment {
        IMAGE_REPO = "mafifdev/autoscale-go-k3s"
        NAMESPACE  = "go-cicd"
        DEPLOYMENT = "go-app"
        CONTAINER  = "go-app"
    }

    options {
        disableConcurrentBuilds()
        timeout(time: 30, unit: 'MINUTES')
    }

    parameters {
        string(name: 'APP_URL', defaultValue: 'http://192.168.123.240:31048/health',
               description: 'Health check URL setelah deploy')
    }

    stages {
        stage('Checkout') {
            steps {
                checkout scm
            }
        }

        stage('Go Test') {
            steps {
                sh 'go test ./...'
            }
        }

        stage('Build Image') {
            steps {
                sh '''
                    IMAGE="${IMAGE_REPO}:${BUILD_NUMBER}"
                    docker build -t "${IMAGE}" -t "${IMAGE_REPO}:latest" .
                '''
            }
        }

        stage('Trivy Image Scan') {
            steps {
                sh '''
                    trivy image \
                      --exit-code 1 \
                      --severity HIGH,CRITICAL \
                      --ignore-unfixed \
                      ${IMAGE_REPO}:${BUILD_NUMBER}
                '''
            }
        }

        stage('Push Image') {
            steps {
                withCredentials([
                    usernamePassword(
                        credentialsId: 'dockerhub',
                        usernameVariable: 'DOCKER_USERNAME',
                        passwordVariable: 'DOCKER_PASSWORD'
                    )
                ]) {
                    sh '''
                        IMAGE="${IMAGE_REPO}:${BUILD_NUMBER}"
                        echo "$DOCKER_PASSWORD" | docker login -u "$DOCKER_USERNAME" --password-stdin
                        docker push "${IMAGE}"
                        docker push "${IMAGE_REPO}:latest"
                        docker logout
                    '''
                }
            }
        }

        stage('Deploy to K3s') {
            steps {
                sh '''
                    kubectl apply -f k8s/service.yaml
                    kubectl apply -f k8s/keda.yaml
                    kubectl set image deployment/${DEPLOYMENT} \
                        ${CONTAINER}=${IMAGE_REPO}:${BUILD_NUMBER} -n ${NAMESPACE}
                    kubectl rollout status deployment/${DEPLOYMENT} \
                        -n ${NAMESPACE} --timeout=120s
                '''
            }
        }

        stage('Health Check') {
            steps {
                sh '''
                    sleep 5
                    curl --fail \
                        --retry 10 \
                        --retry-delay 2 \
                        --retry-connrefused \
                        http://192.168.123.240:31048/health
                '''
            }
        }
    }

    post {
        always {
            sh 'docker logout || true'
        }
        failure {
            echo 'Pipeline failed.'
        }
    }
}
